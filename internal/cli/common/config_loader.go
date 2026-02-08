package common

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
)

// LoadConfigWithWarnings loads the configuration from the --manifest flag and displays warnings.
func LoadConfigWithWarnings(cmd *cobra.Command, pr ui.Printer) (*manifest.Config, error) {
	file, err := ResolveManifestPath(cmd, pr, ".", 3)
	if err != nil {
		return nil, err
	}
	if file != "" {
		_ = cmd.Flags().Set("manifest", file)
	}

	cfg, missing, err := manifest.LoadWithWarnings(file)
	if err == nil {
		for _, name := range missing {
			pr.Warn("environment variable %s is not set; replacing with empty string", name)
		}
		return &cfg, nil
	}
	return nil, err
}

// ResolveManifestPath determines the manifest path to load.
// If --manifest is set, it is returned as-is.
// If omitted and a manifest exists in CWD defaults, returns empty string (loader defaults apply).
// If omitted and no CWD manifest exists, it attempts discovery and interactive selection.
func ResolveManifestPath(cmd *cobra.Command, pr ui.Printer, root string, maxDepth int) (string, error) {
	file, _ := cmd.Flags().GetString("manifest")
	if strings.TrimSpace(file) != "" {
		return file, nil
	}

	hasManifest, err := hasManifestInCurrentDir(".")
	if err != nil {
		return "", err
	}
	if hasManifest {
		return "", nil
	}

	selectedPath, ok, err := SelectManifestPath(cmd, pr, root, maxDepth)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", apperr.New("ResolveManifestPath", apperr.NotFound, "no manifest file found")
	}
	return selectedPath, nil
}

func hasManifestInCurrentDir(dir string) (bool, error) {
	candidates := []string{"dockform.yml", "dockform.yaml", "Dockform.yml", "Dockform.yaml"}
	for _, name := range candidates {
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err == nil && !info.IsDir() {
			return true, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}

// SelectManifestPath scans for manifest files up to maxDepth and presents an interactive picker
// when attached to a TTY. Returns the chosen manifest path and whether a selection was made.
func SelectManifestPath(cmd *cobra.Command, pr ui.Printer, root string, maxDepth int) (string, bool, error) {
	tty := detectTTY(cmd)

	// Discover manifest files
	files, err := findManifestFiles(root, maxDepth)
	if err != nil {
		return "", false, err
	}
	if len(files) == 0 {
		return "", false, nil
	}
	if len(files) == 1 {
		return files[0], true, nil
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

	if !tty.In || !tty.Out {
		return "", false, apperr.New(
			"SelectManifestPath",
			apperr.InvalidInput,
			"multiple manifest files found; re-run with --manifest <path> to select one",
		)
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
