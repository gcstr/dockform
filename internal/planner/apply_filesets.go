package planner

import (
	"bytes"
	"context"
	"path"
	"sort"
	"strings"

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

		// Apply ownership if configured
		if err := fm.applyOwnership(ctx, name, fileset, diff); err != nil {
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

// applyOwnership applies ownership and permission settings to fileset files after they are synced.
func (fm *FilesetManager) applyOwnership(ctx context.Context, name string, fileset manifest.FilesetSpec, diff filesets.Diff) error {
	log := logger.FromContext(ctx).With("component", "fileset")

	// Skip if no ownership configured
	if fileset.Ownership == nil {
		return nil
	}

	ownership := fileset.Ownership

	// Skip if nothing is set
	if ownership.User == "" && ownership.Group == "" && ownership.FileMode == "" && ownership.DirMode == "" {
		return nil
	}

	if fm.progress != nil {
		fm.progress.SetAction("applying ownership for fileset " + name)
	}

	// Build the script to run in the helper container
	script, err := buildOwnershipScript(fileset.TargetPath, ownership, diff)
	if err != nil {
		return apperr.Wrap("filesetmanager.applyOwnership", apperr.Internal, err, "build ownership script for %s", name)
	}

	// Execute the script
	result, err := fm.docker.RunVolumeScript(ctx, fileset.TargetVolume, fileset.TargetPath, script, nil)
	if err != nil {
		log.Warn("ownership application failed", "fileset", name, "error", err.Error())
		if result.Stderr != "" {
			log.Warn("ownership script stderr", "fileset", name, "stderr", result.Stderr)
		}
		return apperr.Wrap("filesetmanager.applyOwnership", apperr.External, err, "apply ownership for fileset %s", name)
	}

	// Log warnings from stdout if any
	if result.Stdout != "" {
		for _, line := range util.SplitNonEmptyLines(result.Stdout) {
			if strings.Contains(strings.ToLower(line), "warning") || strings.Contains(strings.ToLower(line), "error") {
				log.Warn("ownership script output", "fileset", name, "message", line)
			}
		}
	}

	// Log all stderr output (warnings go here)
	if result.Stderr != "" {
		for _, line := range util.SplitNonEmptyLines(result.Stderr) {
			log.Warn("ownership script warning", "fileset", name, "message", line)
		}
	}

	// Count operations for logging
	filesChanged := 0
	dirsChanged := 0
	if ownership.PreserveExisting {
		filesChanged = len(diff.ToCreate) + len(diff.ToUpdate)
		// Count directories in the changed paths
		seen := make(map[string]struct{})
		for _, f := range diff.ToCreate {
			dir := util.DirPath(f.Path)
			if dir != "" && dir != "." {
				seen[dir] = struct{}{}
			}
		}
		for _, f := range diff.ToUpdate {
			dir := util.DirPath(f.Path)
			if dir != "" && dir != "." {
				seen[dir] = struct{}{}
			}
		}
		dirsChanged = len(seen)
	}

	log.Info("ownership applied",
		"fileset", name,
		"preserve_existing", ownership.PreserveExisting,
		"files_affected", filesChanged,
		"dirs_affected", dirsChanged)

	return nil
}

// buildOwnershipScript generates a shell script to apply ownership and permissions.
// The script operates on paths at targetPath (the volume is mounted there by RunVolumeScript).
func buildOwnershipScript(targetPath string, ownership *manifest.Ownership, diff filesets.Diff) (string, error) {
	// Validate target path safety
	cleanPath := path.Clean(targetPath)
	if cleanPath == "/" || cleanPath == "." {
		return "", apperr.New("planner.buildOwnershipScript", apperr.InvalidInput, "unsafe target path: %s", targetPath)
	}
	if strings.Contains(cleanPath, "..") {
		return "", apperr.New("planner.buildOwnershipScript", apperr.InvalidInput, "target path contains ..: %s", targetPath)
	}

	// The volume is mounted at targetPath in the helper container (same as ExtractTarToVolume)
	rootPath := cleanPath

	var script strings.Builder
	script.WriteString("set -e\n") // Exit on error

	// Resolve user and group IDs
	var uid, gid string
	if ownership.User != "" {
		script.WriteString("# Resolve user ID\n")
		// Check if numeric
		if isNumeric(ownership.User) {
			uid = ownership.User
			script.WriteString("UID_VAL='" + shellEscape(uid) + "'\n")
		} else {
			// Try to resolve name (escape the username)
			escapedUser := shellEscape(ownership.User)
			script.WriteString("if getent passwd '" + escapedUser + "' >/dev/null 2>&1; then\n")
			script.WriteString("  UID_VAL=$(getent passwd '" + escapedUser + "' | cut -d: -f3)\n")
			script.WriteString("else\n")
			script.WriteString("  echo 'WARNING: user \"" + escapedUser + "\" not found in helper image; skipping chown'\n")
			script.WriteString("  UID_VAL=''\n")
			script.WriteString("fi\n")
		}
	}

	if ownership.Group != "" {
		script.WriteString("# Resolve group ID\n")
		if isNumeric(ownership.Group) {
			gid = ownership.Group
			script.WriteString("GID_VAL='" + shellEscape(gid) + "'\n")
		} else {
			// Try to resolve name (escape the group name)
			escapedGroup := shellEscape(ownership.Group)
			script.WriteString("if getent group '" + escapedGroup + "' >/dev/null 2>&1; then\n")
			script.WriteString("  GID_VAL=$(getent group '" + escapedGroup + "' | cut -d: -f3)\n")
			script.WriteString("else\n")
			script.WriteString("  echo 'WARNING: group \"" + escapedGroup + "\" not found in helper image; skipping chown'\n")
			script.WriteString("  GID_VAL=''\n")
			script.WriteString("fi\n")
		}
	}

	// Determine paths to operate on
	if ownership.PreserveExisting {
		// Build list of updated files and directories
		updatedFiles := []string{}
		updatedDirs := map[string]struct{}{}

		for _, f := range diff.ToCreate {
			fullPath := path.Join(rootPath, f.Path)
			updatedFiles = append(updatedFiles, fullPath)
			// Add parent directories
			dir := path.Dir(fullPath)
			for dir != rootPath && dir != "/mnt" && dir != "/" {
				updatedDirs[dir] = struct{}{}
				dir = path.Dir(dir)
			}
		}
		for _, f := range diff.ToUpdate {
			fullPath := path.Join(rootPath, f.Path)
			updatedFiles = append(updatedFiles, fullPath)
			// Add parent directories
			dir := path.Dir(fullPath)
			for dir != rootPath && dir != "/mnt" && dir != "/" {
				updatedDirs[dir] = struct{}{}
				dir = path.Dir(dir)
			}
		}

		// Apply directory mode to updated directories
		if ownership.DirMode != "" && len(updatedDirs) > 0 {
			script.WriteString("# Apply directory mode to updated directories\n")
			escapedDirMode := shellEscape(ownership.DirMode)
			for dir := range updatedDirs {
				escapedDir := shellEscape(dir)
				script.WriteString("[ -d '" + escapedDir + "' ] && chmod '" + escapedDirMode + "' '" + escapedDir + "' 2>/dev/null || true\n")
			}
		}

		// Apply file mode to updated files
		if ownership.FileMode != "" && len(updatedFiles) > 0 {
			script.WriteString("# Apply file mode to updated files\n")
			escapedFileMode := shellEscape(ownership.FileMode)
			for _, f := range updatedFiles {
				escapedFile := shellEscape(f)
				script.WriteString("[ -f '" + escapedFile + "' ] && chmod '" + escapedFileMode + "' '" + escapedFile + "' 2>/dev/null || true\n")
			}
		}

		// Apply ownership to updated paths
		if ownership.User != "" || ownership.Group != "" {
			script.WriteString("# Apply ownership to updated paths\n")
			script.WriteString("if [ -n \"${UID_VAL:-}\" ] && [ -n \"${GID_VAL:-}\" ]; then\n")
			allPaths := updatedFiles
			for dir := range updatedDirs {
				allPaths = append(allPaths, dir)
			}
			for _, p := range allPaths {
				escapedPath := shellEscape(p)
				script.WriteString("  [ -e '" + escapedPath + "' ] && chown \"$UID_VAL:$GID_VAL\" '" + escapedPath + "' 2>/dev/null || true\n")
			}
			script.WriteString("elif [ -n \"${UID_VAL:-}\" ]; then\n")
			for _, p := range allPaths {
				escapedPath := shellEscape(p)
				script.WriteString("  [ -e '" + escapedPath + "' ] && chown \"$UID_VAL\" '" + escapedPath + "' 2>/dev/null || true\n")
			}
			script.WriteString("elif [ -n \"${GID_VAL:-}\" ]; then\n")
			for _, p := range allPaths {
				escapedPath := shellEscape(p)
				script.WriteString("  [ -e '" + escapedPath + "' ] && chown \":$GID_VAL\" '" + escapedPath + "' 2>/dev/null || true\n")
			}
			script.WriteString("fi\n")
		}
	} else {
		// Recursive mode: apply to entire subtree
		script.WriteString("# Apply to entire subtree\n")
		escapedRootPath := shellEscape(rootPath)

		// Apply directory mode
		if ownership.DirMode != "" {
			escapedDirMode := shellEscape(ownership.DirMode)
			script.WriteString("find '" + escapedRootPath + "' -type d -exec chmod '" + escapedDirMode + "' {} + 2>/dev/null || true\n")
		}

		// Apply file mode
		if ownership.FileMode != "" {
			escapedFileMode := shellEscape(ownership.FileMode)
			script.WriteString("find '" + escapedRootPath + "' -type f -exec chmod '" + escapedFileMode + "' {} + 2>/dev/null || true\n")
		}

		// Apply ownership recursively
		if ownership.User != "" || ownership.Group != "" {
			script.WriteString("if [ -n \"${UID_VAL:-}\" ] && [ -n \"${GID_VAL:-}\" ]; then\n")
			script.WriteString("  chown -R \"$UID_VAL:$GID_VAL\" '" + escapedRootPath + "' 2>/dev/null || true\n")
			script.WriteString("elif [ -n \"${UID_VAL:-}\" ]; then\n")
			script.WriteString("  chown -R \"$UID_VAL\" '" + escapedRootPath + "' 2>/dev/null || true\n")
			script.WriteString("elif [ -n \"${GID_VAL:-}\" ]; then\n")
			script.WriteString("  chown -R \":$GID_VAL\" '" + escapedRootPath + "' 2>/dev/null || true\n")
			script.WriteString("fi\n")
		}
	}

	script.WriteString("echo 'Ownership applied successfully'\n")

	return script.String(), nil
}

// isNumeric checks if a string contains only digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// shellEscape escapes a string for safe use in a shell script within single quotes.
// It replaces any single quotes with '\” (end quote, escaped quote, start quote).
func shellEscape(s string) string {
	// Replace ' with '\''
	return strings.ReplaceAll(s, "'", "'\\''")
}
