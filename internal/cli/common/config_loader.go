package common

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/goccy/go-yaml"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// LoadConfigWithWarnings loads the configuration from the --config flag and displays warnings.
func LoadConfigWithWarnings(cmd *cobra.Command, pr ui.Printer) (*manifest.Config, error) {
	file, _ := cmd.Flags().GetString("config")
	cfg, missing, err := manifest.LoadWithWarnings(file)
	if err == nil {
		for _, name := range missing {
			pr.Warn("environment variable %s is not set; replacing with empty string", name)
		}
		return &cfg, nil
	}

	// If no config found in CWD and no explicit --config, try interactive discovery
	if file == "" && apperr.IsKind(err, apperr.NotFound) {
		targetCtx, _ := cmd.Flags().GetString("context")
		selectedPath, ok, selErr := SelectManifestPath(cmd, pr, ".", 3, targetCtx)
		if selErr != nil {
			return nil, selErr
		}
		if ok && selectedPath != "" {
			// Propagate selection to the flag so downstream uses the same path
			_ = cmd.Flags().Set("config", selectedPath)
			cfg2, missing2, err2 := manifest.LoadWithWarnings(selectedPath)
			if err2 != nil {
				return nil, err2
			}
			for _, name := range missing2 {
				pr.Warn("environment variable %s is not set; replacing with empty string", name)
			}
			return &cfg2, nil
		}
	}

	return nil, err
}

// SelectManifestPath scans for manifest files up to maxDepth and presents an interactive picker
// of docker.context values when attached to a TTY. Returns the chosen manifest path and whether
// a selection was made. On non-TTY, returns ok=false with no error.
func SelectManifestPath(cmd *cobra.Command, pr ui.Printer, root string, maxDepth int, targetCtx string) (string, bool, error) {
	// Check TTY
	inTTY := false
	outTTY := false
	if f, ok := cmd.InOrStdin().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		inTTY = true
	}
	if f, ok := cmd.OutOrStdout().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		outTTY = true
	}

	// Discover manifest files
	files, err := findManifestFiles(root, maxDepth)
	if err != nil {
		return "", false, err
	}
	if len(files) == 0 {
		return "", false, nil
	}

	// Build labels by reading context names from each file
	labels := make([]string, 0, len(files))
	for _, p := range files {
		lb := readDaemonContextLabels(p)
		if strings.TrimSpace(lb) == "" {
			lb = filepath.Base(filepath.Dir(p))
		}
		labels = append(labels, lb)
	}

	// If target context is provided, try to match it
	if targetCtx != "" {
		for i, lb := range labels {
			if lb == targetCtx {
				return files[i], true, nil
			}
		}
		return "", false, apperr.New("SelectManifestPath", apperr.NotFound, "context '%s' not found", targetCtx)
	}

	if !inTTY || !outTTY {
		return "", false, nil
	}

	// Show picker
	idx, ok, err := ui.SelectOneTTY(cmd.InOrStdin(), cmd.OutOrStdout(), "Target context:", labels)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	if idx < 0 || idx >= len(files) {
		return "", false, nil
	}
	return files[idx], true, nil
}

func findManifestFiles(root string, maxDepth int) ([]string, error) {
	var out []string
	base, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Enforce max depth
		rel, rerr := filepath.Rel(base, path)
		if rerr == nil {
			depth := 0
			if rel != "." {
				depth = strings.Count(rel, string(os.PathSeparator))
			}
			if d.IsDir() && depth > maxDepth {
				return filepath.SkipDir
			}
		}
		if d.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		switch name {
		case "dockform.yml", "dockform.yaml", "Dockform.yml", "Dockform.yaml":
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// readDaemonContextLabels reads context names from a manifest file for display.
func readDaemonContextLabels(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var tmp struct {
		Contexts map[string]struct {
		} `yaml:"contexts"`
	}
	if yerr := yaml.Unmarshal([]byte(b), &tmp); yerr != nil {
		return ""
	}
	// Return comma-separated list of context names
	var names []string
	for name := range tmp.Contexts {
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names, ", ")
}
