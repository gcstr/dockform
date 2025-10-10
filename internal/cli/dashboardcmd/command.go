package dashboardcmd

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/gcstr/dockform/internal/cli/buildinfo"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/data"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/spf13/cobra"
)

// New creates the `dockform dashboard` command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Launch the Dockform dashboard (fullscreen TUI)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := common.SetupCLIContext(cmd)
			if err != nil {
				return err
			}

			loader, err := data.NewLoader(cliCtx.Config, cliCtx.Docker)
			if err != nil {
				return err
			}
			stacks, err := loader.StackSummaries(cliCtx.Ctx)
			if err != nil {
				return err
			}

			identifier := ""
			if cliCtx.Config != nil {
				identifier = cliCtx.Config.Docker.Identifier
			}
			manifestPath := resolveManifestPath(cmd, cliCtx.Config)
			contextName := dockerContextName(cliCtx.Config)
			dockerHost := dockerHostFromClient(cliCtx)
			engineVersion := engineVersionFromClient(cliCtx)

			m := newModel(stacks, buildinfo.Version(), identifier, manifestPath, contextName, dockerHost, engineVersion)
			m.statusProvider = data.NewStatusProvider(cliCtx.Docker, identifier)

			p := tea.NewProgram(m, tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}
	return cmd
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var manifestFilenames = []string{"dockform.yml", "dockform.yaml", "Dockform.yml", "Dockform.yaml"}

func dockerContextName(cfg *manifest.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Docker.Context)
}

func dockerHostFromClient(cliCtx *common.CLIContext) string {
	if cliCtx == nil || cliCtx.Docker == nil {
		return ""
	}
	host, err := cliCtx.Docker.ContextHost(cliCtx.Ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(host)
}

func engineVersionFromClient(cliCtx *common.CLIContext) string {
	if cliCtx == nil || cliCtx.Docker == nil {
		return ""
	}
	ver, err := cliCtx.Docker.ServerVersion(cliCtx.Ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(ver)
}

func resolveManifestPath(cmd *cobra.Command, cfg *manifest.Config) string {
	flagVal, _ := cmd.Flags().GetString("config")
	flagVal = strings.TrimSpace(flagVal)
	baseDir := ""
	if cfg != nil {
		baseDir = cfg.BaseDir
	}

	if path := manifestPathFromInput(flagVal, baseDir); path != "" {
		return path
	}
	if path := manifestPathFromInput(baseDir, baseDir); path != "" {
		return path
	}
	if baseDir != "" {
		return filepath.Clean(baseDir)
	}
	return ""
}

func manifestPathFromInput(input, baseDir string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	resolvedCandidates := candidatePaths(input, baseDir)
	for _, candidate := range resolvedCandidates {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() {
			if path := findManifestInDir(candidate); path != "" {
				return path
			}
			continue
		}
		return absPath(candidate)
	}
	return ""
}

func candidatePaths(input, baseDir string) []string {
	clean := expandUser(input)
	clean = filepath.Clean(clean)

	paths := []string{clean}
	if filepath.IsAbs(clean) {
		return dedupePaths(paths)
	}
	if abs, err := filepath.Abs(clean); err == nil {
		paths = append(paths, abs)
	}
	if baseDir != "" {
		paths = append(paths, filepath.Join(baseDir, clean))
	}
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, clean))
	}
	return dedupePaths(paths)
}

func findManifestInDir(dir string) string {
	for _, name := range manifestFilenames {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return absPath(candidate)
		}
	}
	return ""
}

func expandUser(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func dedupePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func absPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}
