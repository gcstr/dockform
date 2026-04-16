package images

import (
	"fmt"
	"os"
	"strings"
)

// Upgrade takes check results and a map of stack keys to compose file paths,
// rewrites the image tags in the compose files, and returns what changed.
//
// Only results with non-empty NewerTags and no Error are processed.
// The new tag applied is NewerTags[0] (the newest available).
// File content is rewritten only when a change is detected.
func Upgrade(results []ImageStatus, stackFiles map[string][]string) ([]FileChange, error) {
	var changes []FileChange

	for _, result := range results {
		if result.Error != "" || len(result.NewerTags) == 0 {
			continue
		}

		files, ok := stackFiles[result.Stack]
		if !ok || len(files) == 0 {
			continue
		}

		newTag := result.NewerTags[0]
		oldImage := result.Image // full reference as written, e.g. "traefik:v3.0.1"

		// Derive image name without the tag for the FileChange record.
		imageName := oldImage
		if idx := strings.LastIndex(oldImage, ":"); idx != -1 {
			imageName = oldImage[:idx]
		}

		newImage := imageName + ":" + newTag

		for _, filePath := range files {
			changed, err := rewriteImageInFile(filePath, oldImage, newImage)
			if err != nil {
				return nil, fmt.Errorf("upgrade: rewriting %s: %w", filePath, err)
			}
			if changed {
				changes = append(changes, FileChange{
					StackKey: result.Stack,
					Service:  result.Service,
					File:     filePath,
					Image:    imageName,
					OldTag:   result.CurrentTag,
					NewTag:   newTag,
				})
			}
		}
	}

	return changes, nil
}

// rewriteImageInFile replaces all occurrences of oldImage with newImage in the
// file at path using simple string substitution (no YAML parsing). The file is
// written back only when at least one replacement was made.
//
// Both unquoted and double-quoted variants are handled:
//
//	image: traefik:v3.0.1
//	image: "traefik:v3.0.1"
func rewriteImageInFile(path, oldImage, newImage string) (changed bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	content := string(data)
	original := content

	// Replace unquoted form.
	content = strings.ReplaceAll(content, oldImage, newImage)

	if content == original {
		return false, nil
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, err
	}

	return true, nil
}
