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
	return &FilesetManager{docker: docker, progress: orNop(progress)}
}

// NewFilesetManagerWithClient creates a new fileset manager with a specific client.
func NewFilesetManagerWithClient(client DockerClient, progress ProgressReporter) *FilesetManager {
	return &FilesetManager{docker: client, progress: orNop(progress)}
}

// SyncFilesetsForContext synchronizes filesets for a specific context into their target volumes.
// Returns services that need restart.
func (fm *FilesetManager) SyncFilesetsForContext(ctx context.Context, cfg manifest.Config, contextName string, existingVolumes map[string]struct{}, execCtx *ContextExecutionContext) (map[restartTarget]struct{}, error) {
	log := logger.FromContext(ctx).With("component", "fileset", "context", contextName)
	restartPending := map[restartTarget]struct{}{}

	// Built once per context: attached discovery needs to map a container's
	// compose project back to the stack key that owns it.
	projectToStack := stackProjectMap(ctx, fm.docker, cfg.GetStacksForContext(contextName))
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
		ref := ResourceRef{Context: contextName, Type: ResourceFileset, Name: name}

		// Exactly one Start per fileset, emitted before any Detail or Fail on
		// this ref can fire. Every exit path below must reach exactly one of
		// Finish or Fail to close out this Start.
		fm.progress.Start(ref, "syncing")

		if fileset.SourceAbs == "" {
			err := apperr.New("filesetmanager.SyncFilesetsForContext", apperr.InvalidInput, "fileset %s: resolved source path is empty", name)
			fm.progress.Fail(ref, err)
			return nil, err
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
				wrapped := apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.Internal, err, "index local filesets for %s", name)
				fm.progress.Fail(ref, wrapped)
				return nil, wrapped
			}

			// Only read from volume if it exists to avoid implicit creation
			raw := ""
			if _, volumeExists := existingVolumes[fileset.TargetVolume]; volumeExists {
				raw, err = fm.docker.ReadFileFromVolume(ctx, fileset.TargetVolume, fileset.TargetPath, filesets.IndexFileName)
				if err != nil {
					wrapped := apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, err, "read index file for fileset %s", name)
					fm.progress.Fail(ref, wrapped)
					return nil, wrapped
				}
			}
			remote, err = filesets.ParseIndexJSON(raw)
			if err != nil {
				wrapped := apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, err, "parse remote index for fileset %s", name)
				fm.progress.Fail(ref, wrapped)
				return nil, wrapped
			}
			diff = filesets.DiffIndexes(local, remote)
		}

		// If completely equal, skip this fileset. This fileset was never in the
		// plan's own seed (SeedRefs omits no-op filesets), so an empty Finish
		// result here is the "found nothing to do" signal both renderers treat
		// as void-this-line: it keeps an unchanged fileset from becoming a
		// permanent "(discovered)" line that inflates the running total, while
		// the Start above — which stays, deliberately — still names the fileset
		// if reading its remote index (a real, sometimes slow, docker call)
		// fails.
		if local.TreeHash == remote.TreeHash {
			st := logger.StartStep(log, "fileset_sync", name, "resource_kind", "fileset", "target_volume", fileset.TargetVolume)
			st.OK(false) // No changes needed
			fm.progress.Finish(ref, "")
			continue
		}

		// Determine apply mode (default hot)
		isCold := fileset.ApplyMode == "cold"

		// Compute target services to restart/stop based on restart_services semantics
		targetServices, err := resolveTargetServices(ctx, fm.docker, fileset, projectToStack)
		if err != nil {
			wrapped := apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, err, "resolve target services for fileset %s", name)
			fm.progress.Fail(ref, wrapped)
			return nil, wrapped
		}

		// For cold mode, stop targets (if any) before syncing
		var stoppedContainers []string
		if isCold && len(targetServices) > 0 {
			fm.progress.Detail(ref, "stopping services")
			// Get all containers and find ones matching the target services
			items, err := fm.docker.ListComposeContainersAll(ctx)
			if err != nil {
				wrapped := apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, err, "list compose containers for cold fileset %s", name)
				fm.progress.Fail(ref, wrapped)
				return nil, wrapped
			}
			var containersToStop []string
			for _, t := range targetServices {
				if t.Service == "" {
					continue
				}
				for _, it := range items {
					if it.Service == t.Service {
						containersToStop = append(containersToStop, it.Name)
						stoppedContainers = append(stoppedContainers, it.Name)
						break
					}
				}
			}
			if len(containersToStop) > 0 {
				if err := fm.docker.StopContainers(ctx, containersToStop); err != nil {
					wrapped := apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, err, "stop cold-mode containers for fileset %s", name)
					fm.progress.Fail(ref, wrapped)
					return nil, wrapped
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
		if err := fm.syncFilesetFiles(ctx, ref, fileset, diff); err != nil {
			wrapped := restartColdContainersOnFailure(err)
			fm.progress.Fail(ref, wrapped)
			return nil, st.Fail(wrapped)
		}

		// Delete removed files
		if err := fm.deleteFilesetFiles(ctx, ref, fileset, diff); err != nil {
			wrapped := restartColdContainersOnFailure(err)
			fm.progress.Fail(ref, wrapped)
			return nil, st.Fail(wrapped)
		}

		// Write updated index
		if err := fm.writeFilesetIndex(ctx, ref, fileset, local); err != nil {
			wrapped := restartColdContainersOnFailure(err)
			fm.progress.Fail(ref, wrapped)
			return nil, st.Fail(wrapped)
		}

		// Apply ownership if configured
		if err := fm.applyOwnership(ctx, contextName, name, fileset, diff); err != nil {
			wrapped := restartColdContainersOnFailure(err)
			fm.progress.Fail(ref, wrapped)
			return nil, st.Fail(wrapped)
		}

		// For cold mode, start previously stopped containers again
		if isCold && len(stoppedContainers) > 0 {
			fm.progress.Detail(ref, "starting services")
			if err := fm.docker.StartContainers(ctx, stoppedContainers); err != nil {
				wrapped := apperr.Wrap("filesetmanager.SyncFilesetsForContext", apperr.External, err, "restart cold-mode containers for fileset %s", name)
				fm.progress.Fail(ref, wrapped)
				return nil, st.Fail(wrapped)
			}
		}

		st.OK(true) // Fileset was successfully synced
		fm.progress.Finish(ref, "synced")

		// Queue services for restart only for hot mode
		if !isCold {
			for _, t := range targetServices {
				if t.Service != "" {
					restartPending[t] = struct{}{}
				}
			}
		}
	}

	return restartPending, nil
}

// syncFilesetFiles handles create and update operations for fileset files.
func (fm *FilesetManager) syncFilesetFiles(ctx context.Context, ref ResourceRef, fileset manifest.FilesetSpec, diff filesets.Diff) error {
	name := ref.Name

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

	fm.progress.Detail(ref, "uploading files")

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
func (fm *FilesetManager) deleteFilesetFiles(ctx context.Context, ref ResourceRef, fileset manifest.FilesetSpec, diff filesets.Diff) error {
	name := ref.Name

	if len(diff.ToDelete) == 0 {
		return nil
	}

	fm.progress.Detail(ref, "deleting files")

	if err := fm.docker.RemovePathsFromVolume(ctx, fileset.TargetVolume, fileset.TargetPath, diff.ToDelete); err != nil {
		return apperr.Wrap("filesetmanager.deleteFilesetFiles", apperr.External, err, "delete files for fileset %s", name)
	}

	return nil
}

// writeFilesetIndex writes the updated index file to the volume.
func (fm *FilesetManager) writeFilesetIndex(ctx context.Context, ref ResourceRef, fileset manifest.FilesetSpec, index filesets.Index) error {
	name := ref.Name

	fm.progress.Detail(ref, "writing index")

	jsonStr, err := index.ToJSON()
	if err != nil {
		return apperr.Wrap("filesetmanager.writeFilesetIndex", apperr.Internal, err, "encode index for %s", name)
	}

	if err := fm.docker.WriteFileToVolume(ctx, fileset.TargetVolume, fileset.TargetPath, filesets.IndexFileName, jsonStr); err != nil {
		return apperr.Wrap("filesetmanager.writeFilesetIndex", apperr.External, err, "write index for fileset %s", name)
	}

	return nil
}
