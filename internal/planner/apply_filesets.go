package planner

import (
	"bytes"
	"context"
	"sort"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/util"
)

// FilesetManager handles synchronization of filesets into Docker volumes.
type FilesetManager struct {
	docker   DockerClient
	progress ProgressReporter
}

// NewFilesetManager creates a new fileset manager.
func NewFilesetManager(docker DockerClient, progress ProgressReporter) *FilesetManager {
	return &FilesetManager{docker: docker, progress: progress}
}

// NewFilesetManagerWithClient creates a new fileset manager with a specific client.
func NewFilesetManagerWithClient(client DockerClient, progress ProgressReporter) *FilesetManager {
	return &FilesetManager{docker: client, progress: progress}
}

// SyncFilesetsForContext synchronizes filesets for a specific context into their target volumes.
// Returns services that need restart.
func (fm *FilesetManager) SyncFilesetsForContext(ctx context.Context, cfg manifest.Config, contextName string, existingVolumes map[string]struct{}, execCtx *ContextExecutionContext) (map[string]struct{}, error) {
	log := logger.FromContext(ctx).With("component", "fileset", "context", contextName)
	restartPending := map[string]struct{}{}
	if fm.docker == nil {
		return nil, apperr.New("filesetmanager.SyncFilesetsForContext", apperr.Precondition, "docker client not configured")
	}

	// Get filesets for this context
	contextFilesets := cfg.GetFilesetsForContext(contextName)
	if len(contextFilesets) == 0 {
		return restartPending, nil
	}

	// Process filesets in deterministic order
	filesetNames := make([]string, 0, len(contextFilesets))
	for name := range contextFilesets {
		filesetNames = append(filesetNames, name)
	}
	sort.Strings(filesetNames)

	for _, name := range filesetNames {
		fileset := contextFilesets[name]

		if fileset.SourceAbs == "" {
			return nil, apperr.New("filesetmanager.SyncFilesetsForContext", apperr.InvalidInput, "fileset %s: resolved source path is empty", name)
		}

		var local, remote filesets.Index
		var diff filesets.Diff

		// Try to reuse cached data from plan execution context
		if execCtx != nil && execCtx.Filesets != nil && execCtx.Filesets[name] != nil {
			log.Info("fileset_sync_reuse_cache", "fileset", name, "msg", "reusing indexes and diff from plan")
			execData := execCtx.Filesets[name]
			local = execData.LocalIndex
			remote = execData.RemoteIndex
			diff = execData.Diff
		} else {
			// Fallback: compute indexes and diff fresh (original behavior)
			var err error
			local, err = filesets.BuildLocalIndex(fileset.SourceAbs, fileset.TargetPath, fileset.Exclude)
			if err != nil {
				return nil, apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.Internal, err, "index local filesets for %s", name)
			}

			// Only read from volume if it exists to avoid implicit creation
			raw := ""
			if _, volumeExists := existingVolumes[fileset.TargetVolume]; volumeExists {
				raw, err = fm.docker.ReadFileFromVolume(ctx, fileset.TargetVolume, fileset.TargetPath, filesets.IndexFileName)
				if err != nil {
					return nil, apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, err, "read index file for fileset %s", name)
				}
			}
			remote, err = filesets.ParseIndexJSON(raw)
			if err != nil {
				return nil, apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, err, "parse remote index for fileset %s", name)
			}
			diff = filesets.DiffIndexes(local, remote)
		}

		// If completely equal, skip this fileset
		if local.TreeHash == remote.TreeHash {
			st := logger.StartStep(log, "fileset_sync", name, "resource_kind", "fileset", "target_volume", fileset.TargetVolume)
			st.OK(false) // No changes needed
			continue
		}

		// Determine apply mode (default hot)
		isCold := fileset.ApplyMode == "cold"

		// Compute target services to restart/stop based on restart_services semantics
		targetServices, err := resolveTargetServices(ctx, fm.docker, fileset)
		if err != nil {
			return nil, apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, err, "resolve target services for fileset %s", name)
		}

		// For cold mode, stop targets (if any) before syncing
		var stoppedContainers []string
		if isCold && len(targetServices) > 0 {
			if fm.progress != nil {
				fm.progress.SetAction("stopping services for fileset " + name)
			}
			// Get all containers and find ones matching the target services
			items, err := fm.docker.ListComposeContainersAll(ctx)
			if err != nil {
				return nil, apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, err, "list compose containers for cold fileset %s", name)
			}
			var containersToStop []string
			for _, svc := range targetServices {
				if svc == "" {
					continue
				}
				for _, it := range items {
					if it.Service == svc {
						containersToStop = append(containersToStop, it.Name)
						stoppedContainers = append(stoppedContainers, it.Name)
						break
					}
				}
			}
			if len(containersToStop) > 0 {
				if err := fm.docker.StopContainers(ctx, containersToStop); err != nil {
					return nil, apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, err, "stop cold-mode containers for fileset %s", name)
				}
			}
		}

		restartColdContainersOnFailure := func(baseErr error) error {
			if !isCold || len(stoppedContainers) == 0 {
				return baseErr
			}
			restartErr := fm.docker.StartContainers(ctx, stoppedContainers)
			if restartErr == nil {
				return baseErr
			}
			return apperr.Aggregate(
				"filesetmanager.SyncFilesetsForContext",
				apperr.External,
				"fileset sync failed and cold-mode service restart also failed",
				baseErr,
				apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, restartErr, "restart cold-mode containers for fileset %s", name),
			)
		}

		// Start logging the sync operation
		st := logger.StartStep(log, "fileset_sync", name,
			"resource_kind", "fileset",
			"target_volume", fileset.TargetVolume,
			"apply_mode", fileset.ApplyMode,
			"files_changed", len(diff.ToCreate)+len(diff.ToUpdate),
			"files_deleted", len(diff.ToDelete))

		// Sync files (create + update)
		if err := fm.syncFilesetFiles(ctx, name, fileset, diff); err != nil {
			return nil, st.Fail(restartColdContainersOnFailure(err))
		}

		// Delete removed files
		if err := fm.deleteFilesetFiles(ctx, name, fileset, diff); err != nil {
			return nil, st.Fail(restartColdContainersOnFailure(err))
		}

		// Write updated index
		if err := fm.writeFilesetIndex(ctx, name, fileset, local); err != nil {
			return nil, st.Fail(restartColdContainersOnFailure(err))
		}

		// Apply ownership if configured
		if err := fm.applyOwnership(ctx, name, fileset, diff); err != nil {
			return nil, st.Fail(restartColdContainersOnFailure(err))
		}

		// For cold mode, start previously stopped containers again
		if isCold && len(stoppedContainers) > 0 {
			if fm.progress != nil {
				fm.progress.SetAction("starting services for fileset " + name)
			}
			if err := fm.docker.StartContainers(ctx, stoppedContainers); err != nil {
				return nil, st.Fail(apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, err, "restart cold-mode containers for fileset %s", name))
			}
		}

		st.OK(true) // Fileset was successfully synced

		// Queue services for restart only for hot mode
		if !isCold {
			for _, svc := range targetServices {
				if svc != "" {
					restartPending[svc] = struct{}{}
				}
			}
		}
	}

	return restartPending, nil
}

// syncFilesetFiles handles create and update operations for fileset files.
func (fm *FilesetManager) syncFilesetFiles(ctx context.Context, name string, fileset manifest.FilesetSpec, diff filesets.Diff) error {
	// Build tar for create+update
	paths := make([]string, 0, len(diff.ToCreate)+len(diff.ToUpdate))
	for _, f := range diff.ToCreate {
		paths = append(paths, f.Path)
	}
	for _, f := range diff.ToUpdate {
		paths = append(paths, f.Path)
	}

	if len(paths) == 0 {
		return nil
	}

	// Deterministic order for tar emission
	sort.Strings(paths)

	if fm.progress != nil {
		fm.progress.SetAction("syncing fileset " + name)
	}

	var buf bytes.Buffer
	if err := util.TarFilesToWriter(fileset.SourceAbs, paths, &buf); err != nil {
		return apperr.Wrap("filesetmanager.syncFilesetFiles", apperr.Internal, err, "build tar for fileset %s", name)
	}

	if err := fm.docker.ExtractTarToVolume(ctx, fileset.TargetVolume, fileset.TargetPath, &buf); err != nil {
		return apperr.Wrap("filesetmanager.syncFilesetFiles", apperr.External, err, "extract tar for fileset %s", name)
	}

	return nil
}

// deleteFilesetFiles handles deletion of removed files.
func (fm *FilesetManager) deleteFilesetFiles(ctx context.Context, name string, fileset manifest.FilesetSpec, diff filesets.Diff) error {
	if len(diff.ToDelete) == 0 {
		return nil
	}

	if fm.progress != nil {
		fm.progress.SetAction("deleting files from fileset " + name)
	}

	if err := fm.docker.RemovePathsFromVolume(ctx, fileset.TargetVolume, fileset.TargetPath, diff.ToDelete); err != nil {
		return apperr.Wrap("filesetmanager.deleteFilesetFiles", apperr.External, err, "delete files for fileset %s", name)
	}

	return nil
}

// writeFilesetIndex writes the updated index file to the volume.
func (fm *FilesetManager) writeFilesetIndex(ctx context.Context, name string, fileset manifest.FilesetSpec, index filesets.Index) error {
	if fm.progress != nil {
		fm.progress.SetAction("writing index for fileset " + name)
	}

	jsonStr, err := index.ToJSON()
	if err != nil {
		return apperr.Wrap("filesetmanager.writeFilesetIndex", apperr.Internal, err, "encode index for %s", name)
	}

	if err := fm.docker.WriteFileToVolume(ctx, fileset.TargetVolume, fileset.TargetPath, filesets.IndexFileName, jsonStr); err != nil {
		return apperr.Wrap("filesetmanager.writeFilesetIndex", apperr.External, err, "write index for fileset %s", name)
	}

	return nil
}
