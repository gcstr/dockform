package composecmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

// New creates the top-level `compose` command and wires subcommands
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compose",
		Short: "Work with docker compose files for stacks",
	}
	cmd.AddCommand(newRenderCmd())
	return cmd
}

func newRenderCmd() *cobra.Command {
	var showSecrets bool
	var maskStr string

	cmd := &cobra.Command{
		Use:   "render [stack]",
		Short: "Render a stack's docker compose config fully resolved",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stackName := args[0]
			file, _ := cmd.Flags().GetString("config")
			pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}

			// Load manifest with warnings
			cfg, missing, err := manifest.LoadWithWarnings(file)
			if err != nil {
				return err
			}
			for _, name := range missing {
				pr.Warn("environment variable %s is not set; replacing with empty string", name)
			}

			stack, ok := cfg.Stacks[stackName]
			if !ok {
				return apperr.New("cli.compose.render", apperr.InvalidInput, "unknown stack %q", stackName)
			}

			// Build inline env including SOPS secrets
			detector := planner.NewServiceStateDetector(nil)
			inline := detector.BuildInlineEnv(cmd.Context(), stack, cfg.Sops)

			// Get docker client for the stack's daemon
			// Stack keys are in "context/stack" format, or we use the first context
			var contextName string
			identifier := cfg.Identifier
			parts := strings.SplitN(stackName, "/", 2)
			if len(parts) == 2 {
				if _, ok := cfg.Contexts[parts[0]]; ok {
					contextName = parts[0]
				}
			}
			// Fall back to first context if stack key doesn't have context prefix
			if contextName == "" {
				for name := range cfg.Contexts {
					contextName = name
					break
				}
			}

			// Compose raw config
			docker := dockercli.New(contextName).WithIdentifier(identifier)
			raw, err := docker.ComposeConfigRaw(cmd.Context(), stack.Root, stack.Files, stack.Profiles, stack.EnvFile, inline)
			if err != nil {
				return err
			}

			// Optionally mask secrets from manifest environment/secrets
			if !showSecrets {
				raw = common.MaskSecretsSimple(raw, stack, maskStr)
			}

			// Build a clean display title: relative path from CWD to the first compose file
			// Handle absolute/relative file entries consistently to avoid duplicated prefixes
			var title string
			if len(stack.Files) > 0 {
				first := stack.Files[0]
				abs := first
				if !filepath.IsAbs(first) {
					abs = filepath.Join(stack.Root, first)
				}
				if cwd, err := os.Getwd(); err == nil {
					if rel, err := filepath.Rel(cwd, abs); err == nil {
						title = rel
					} else {
						title = filepath.Clean(abs)
					}
				} else {
					title = filepath.Clean(abs)
				}
				if len(stack.Files) > 1 {
					title = title + " (+" + strconv.Itoa(len(stack.Files)-1) + ")"
				}
			} else {
				title = "compose.yaml"
			}

			return ui.RenderYAMLInPagerTTY(cmd.InOrStdin(), cmd.OutOrStdout(), raw, title)
		},
	}
	cmd.Flags().BoolVar(&showSecrets, "show-secrets", false, "Show secrets inline (dangerous)")
	cmd.Flags().StringVar(&maskStr, "mask", "full", "Secret masking strategy: full|partial|preserve-length")
	return cmd
}
