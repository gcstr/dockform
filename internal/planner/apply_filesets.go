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

// SyncFilesets synchronizes all filesets into their target volumes and returns services that need restart.
func (fm *FilesetManager) SyncFilesets(ctx context.Context, cfg manifest.Config, existingVolumes map[string]struct{}) (map[string]struct{}, error) {
	log := logger.FromContext(ctx).With("component", "fileset")
	restartPending := map[string]struct{}{}
	if fm.docker == nil {
		return nil, apperr.New("filesetmanager.SyncFilesets", apperr.Precondition, "docker client not configured")
	}

	if len(cfg.Filesets) == 0 {
		return restartPending, nil
	}

	// Process filesets in deterministic order
	filesetNames := make([]string, 0, len(cfg.Filesets))
	for name := range cfg.Filesets {
		filesetNames = append(filesetNames, name)
	}
	sort.Strings(filesetNames)

	for _, name := range filesetNames {
		fileset := cfg.Filesets[name]

		if fileset.SourceAbs == "" {
			return nil, apperr.New("filesetmanager.SyncFilesets", apperr.InvalidInput, "fileset %s: resolved source path is empty", name)
		}

		// Build local and remote indexes
		local, err := filesets.BuildLocalIndex(fileset.SourceAbs, fileset.TargetPath, fileset.Exclude)
		if err != nil {
			return nil, apperr.Wrap("filesetmanager.SyncFilesets", apperr.Internal, err, "index local filesets for %s", name)
		}

		// Only read from volume if it exists to avoid implicit creation
		raw := ""
		if _, volumeExists := existingVolumes[fileset.TargetVolume]; volumeExists {
			raw, _ = fm.docker.ReadFileFromVolume(ctx, fileset.TargetVolume, fileset.TargetPath, filesets.IndexFileName)
		}
		remote, _ := filesets.ParseIndexJSON(raw)

		diff := filesets.DiffIndexes(local, remote)

		// If completely equal, skip this fileset
		if local.TreeHash == remote.TreeHash {
			st := logger.StartStep(log, "fileset_sync", name, "resource_kind", "fileset", "target_volume", fileset.TargetVolume)
			st.OK(false) // No changes needed
			continue
		}

		// Determine apply mode (default hot)
		isCold := fileset.ApplyMode == "cold"

		// Compute target services to restart/stop based on restart_services semantics
		targetServices, _ := resolveTargetServices(ctx, fm.docker, fileset)

		// For cold mode, stop targets (if any) before syncing
		var stoppedContainers []string
		if isCold && len(targetServices) > 0 {
			if fm.progress != nil {
				fm.progress.SetAction("stopping services for fileset " + name)
			}
			// Get all containers and find ones matching the target services
			if items, err := fm.docker.ListComposeContainersAll(ctx); err == nil {
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
					_ = fm.docker.StopContainers(ctx, containersToStop)
				}
			}
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
			return nil, st.Fail(err)
		}

		// Delete removed files
		if err := fm.deleteFilesetFiles(ctx, name, fileset, diff); err != nil {
			return nil, st.Fail(err)
		}

		// Write updated index
		if err := fm.writeFilesetIndex(ctx, name, fileset, local); err != nil {
			return nil, st.Fail(err)
		}

		// For cold mode, start previously stopped containers again
		if isCold && len(stoppedContainers) > 0 {
			if fm.progress != nil {
				fm.progress.SetAction("starting services for fileset " + name)
			}
			_ = fm.docker.StartContainers(ctx, stoppedContainers)
		}

		st.OK(true) // Fileset was successfully synced

		if fm.progress != nil {
			fm.progress.Increment()
		}

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
