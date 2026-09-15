package manifest

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/goccy/go-yaml"
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
	fmt.Fprintf(&msg, "stack %s contains bind mounts with relative paths which will not work correctly with remote Docker contexts.\n\n", stackKey)
	msg.WriteString("Bind mounts found:\n")
	for _, bm := range allBindMounts {
		fmt.Fprintf(&msg, "  - %s\n", bm)
	}
	msg.WriteString("\nBind mounts reference paths on the Docker daemon's filesystem, not your local machine.\n")
	msg.WriteString("When using remote Docker contexts, these paths would be resolved on the remote server.\n\n")
	msg.WriteString("Solution: Use Dockform filesets for syncing local files to remote volumes.\n\n")
	msg.WriteString("Migration steps:\n")
	fmt.Fprintf(&msg, "  1. Create a 'volumes/' directory in your stack: %s/%s/volumes/\n", context, stackName)
	msg.WriteString("  2. Move your bind mount directories into volumes/\n")
	fmt.Fprintf(&msg, "     Example: ./config → %s/%s/volumes/config/\n", context, stackName)
	msg.WriteString("  3. Change compose volumes to use named volumes:\n")
	msg.WriteString("     - Old: ./config:/app/config\n")
	fmt.Fprintf(&msg, "     - New: %s_config:/app/config\n", stackName)
	msg.WriteString("  4. Declare the volume in dockform.yaml:\n")
	msg.WriteString("     contexts:\n")
	fmt.Fprintf(&msg, "       %s:\n", context)
	msg.WriteString("         volumes:\n")
	fmt.Fprintf(&msg, "           %s_config: {}\n\n", stackName)
	msg.WriteString("Dockform will auto-discover the fileset and sync files correctly to the remote server.\n")
	msg.WriteString("See: https://github.com/gcstr/dockform#filesets for more information.")

	return apperr.New("manifest.validateBindMounts", apperr.InvalidInput, "%s", msg.String())
}

// detectBindMounts returns the relative bind mount sources declared by any
// service in a compose document.
//
// It parses the YAML rather than matching raw text: the same mount can be
// written quoted, unquoted, or in long form, and a regex sees only one of
// those. Both compose volume syntaxes are handled:
//
//   - ./path:/container/path          (short form, with optional :ro suffix)
//   - {type: bind, source: ./path}    (long form)
//
// Only relative sources are reported. An absolute source is a path on the
// daemon's filesystem, which is what a bind mount is supposed to be, so
// /var/run/docker.sock and friends are left alone. Named volumes have no
// leading ./, ../ or ~/ and are likewise ignored.
//
// A document that does not parse yields no mounts: compose itself rejects it
// later with a better message than this validator could produce.
func detectBindMounts(content string) []string {
	var doc struct {
		Services map[string]struct {
			Volumes []any `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil
	}

	var mounts []string
	seen := make(map[string]bool)
	add := func(source string) {
		if !isRelativeBindSource(source) || seen[source] {
			return
		}
		seen[source] = true
		mounts = append(mounts, source)
	}

	// Sorted so the reported order does not depend on Go's map iteration.
	for _, name := range slices.Sorted(maps.Keys(doc.Services)) {
		for _, entry := range doc.Services[name].Volumes {
			switch v := entry.(type) {
			case string:
				add(shortFormSource(v))
			case map[string]any:
				add(longFormSource(v))
			}
		}
	}
	return mounts
}

// shortFormSource returns the host side of a "source:target[:mode]" entry.
// An entry without a target mounts an anonymous volume at that path and never
// touches the host, so it reports no source.
func shortFormSource(entry string) string {
	source, _, ok := strings.Cut(entry, ":")
	if !ok {
		return ""
	}
	return source
}

// longFormSource returns the source of a long-form entry, but only when it is
// a bind. type: volume names a volume, not a host path.
func longFormSource(entry map[string]any) string {
	if t, _ := entry["type"].(string); t != "bind" {
		return ""
	}
	source, _ := entry["source"].(string)
	return source
}

// isRelativeBindSource reports whether a bind source is resolved against the
// compose file's directory. Those are the ones that break on a remote context:
// compose expands them to a path on the local machine, and the remote daemon
// then creates that path as an empty directory.
func isRelativeBindSource(source string) bool {
	switch source {
	case ".", "..", "~":
		return true
	}
	return strings.HasPrefix(source, "./") ||
		strings.HasPrefix(source, "../") ||
		strings.HasPrefix(source, "~/")
}
