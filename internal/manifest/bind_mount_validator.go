package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
)

// validateBindMountsInComposeFile checks for problematic bind mounts by parsing the raw compose file.
// This avoids needing a Docker client connection during manifest loading.
func validateBindMountsInComposeFile(stackKey string, stack Stack) error {
	if len(stack.Files) == 0 {
		return nil
	}

	// Check all compose files (including overrides) for bind mounts
	var allBindMounts []string
	seenMounts := make(map[string]bool)

	for _, file := range stack.Files {
		composeFile := file
		if !filepath.IsAbs(composeFile) {
			composeFile = filepath.Join(stack.Root, composeFile)
		}

		content, err := os.ReadFile(composeFile)
		if err != nil {
			// If we can't read the file, skip validation (it will fail later during plan/apply)
			continue
		}

		bindMounts := detectBindMounts(string(content))
		for _, mount := range bindMounts {
			if !seenMounts[mount] {
				allBindMounts = append(allBindMounts, mount)
				seenMounts[mount] = true
			}
		}
	}

	if len(allBindMounts) == 0 {
		return nil
	}

	// Build helpful error message
	context, stackName, _ := ParseStackKey(stackKey)

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("stack %s contains bind mounts with relative paths which will not work correctly with remote Docker contexts.\n\n", stackKey))
	msg.WriteString("Bind mounts found:\n")
	for _, bm := range allBindMounts {
		msg.WriteString(fmt.Sprintf("  - %s\n", bm))
	}
	msg.WriteString("\nBind mounts reference paths on the Docker daemon's filesystem, not your local machine.\n")
	msg.WriteString("When using remote Docker contexts, these paths would be resolved on the remote server.\n\n")
	msg.WriteString("Solution: Use Dockform filesets for syncing local files to remote volumes.\n\n")
	msg.WriteString("Migration steps:\n")
	msg.WriteString(fmt.Sprintf("  1. Create a 'volumes/' directory in your stack: %s/%s/volumes/\n", context, stackName))
	msg.WriteString("  2. Move your bind mount directories into volumes/\n")
	msg.WriteString(fmt.Sprintf("     Example: ./config → %s/%s/volumes/config/\n", context, stackName))
	msg.WriteString("  3. Change compose volumes to use named volumes:\n")
	msg.WriteString("     - Old: ./config:/app/config\n")
	msg.WriteString(fmt.Sprintf("     - New: %s_config:/app/config\n", stackName))
	msg.WriteString("  4. Declare the volume in dockform.yaml:\n")
	msg.WriteString("     contexts:\n")
	msg.WriteString(fmt.Sprintf("       %s:\n", context))
	msg.WriteString("         volumes:\n")
	msg.WriteString(fmt.Sprintf("           %s_config: {}\n\n", stackName))
	msg.WriteString("Dockform will auto-discover the fileset and sync files correctly to the remote server.\n")
	msg.WriteString("See: https://github.com/gcstr/dockform#filesets for more information.")

	return apperr.New("manifest.validateBindMounts", apperr.InvalidInput, "%s", msg.String())
}

// detectBindMounts uses regex to find bind mount patterns in compose YAML.
// This is a simple heuristic that catches common patterns like:
//   - ./path:/container/path
//   - ../path:/container/path
//   - ~/path:/container/path (relative to home)
func detectBindMounts(content string) []string {
	var mounts []string
	seen := make(map[string]bool)

	// Pattern matches:
	// - ./something:/path
	// - ../something:/path
	// - ~/something:/path
	// Excludes absolute paths (/something:/path) and named volumes (volumename:/path)
	bindMountPattern := regexp.MustCompile(`(?m)^\s*-\s+(\.{1,2}/[^:\s]+|~/[^:\s]+):`)

	matches := bindMountPattern.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) > 1 {
			mount := strings.TrimSpace(match[1])
			if !seen[mount] {
				mounts = append(mounts, mount)
				seen[mount] = true
			}
		}
	}

	return mounts
}
