package planner

import (
	"context"
	"path"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/util"
)

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
		buildPreserveExistingScript(&script, rootPath, ownership, diff)
	} else {
		buildRecursiveOwnershipScript(&script, rootPath, ownership)
	}

	script.WriteString("echo 'Ownership applied successfully'\n")

	return script.String(), nil
}

// buildPreserveExistingScript generates ownership script for preserve_existing mode.
func buildPreserveExistingScript(script *strings.Builder, rootPath string, ownership *manifest.Ownership, diff filesets.Diff) {
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
}

// buildRecursiveOwnershipScript generates ownership script for recursive mode.
func buildRecursiveOwnershipScript(script *strings.Builder, rootPath string, ownership *manifest.Ownership) {
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
// Deprecated: Use util.ShellEscape instead.
func shellEscape(s string) string {
	return util.ShellEscape(s)
}
