package cli

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

func newManifestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Work with the manifest file",
	}

	cmd.AddCommand(newManifestRenderCmd())
	return cmd
}

func newManifestRenderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render the manifest with environment variables interpolated",
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("config")
			pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
			out, filename, missing, err := manifest.RenderWithWarningsAndPath(file)
			if err != nil {
				// Interactive discovery if no manifest at CWD and no explicit --config
				if file == "" && apperr.IsKind(err, apperr.NotFound) {
					if selPath, ok, selErr := selectManifestPath(cmd, pr, ".", 3); selErr == nil && ok {
						_ = cmd.Flags().Set("config", selPath)
						out, filename, missing, err = manifest.RenderWithWarningsAndPath(selPath)
					} else if selErr != nil {
						return selErr
					}
				}
				if err != nil {
					return err
				}
			}
			for _, name := range missing {
				pr.Warn("environment variable %s is not set; replacing with empty string", name)
			}
			// Render in a full-screen viewport pager when attached to a TTY;
			// otherwise fall back to plain printing to preserve pipes/tests.
			if err := ui.RenderYAMLInPagerTTY(cmd.InOrStdin(), cmd.OutOrStdout(), out, filename); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}

// compose masking strategies
type maskStrategy string

const (
	maskFull           maskStrategy = "full"
	maskPartial        maskStrategy = "partial"
	maskPreserveLength maskStrategy = "preserve-length"
)

func newComposeRenderCmd() *cobra.Command {
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

			// Compose raw config
			docker := dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
			raw, err := docker.ComposeConfigRaw(cmd.Context(), stack.Root, stack.Files, stack.Profiles, stack.EnvFile, inline)
			if err != nil {
				return err
			}

			// Optionally mask secrets from manifest environment/secrets
			if !showSecrets {
				raw = maskSecretsSimple(raw, stack, maskStrategy(maskStr))
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

// newComposeCmd creates the top-level `compose` command and wires subcommands
func newComposeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compose",
		Short: "Work with docker compose files for stacks",
	}
	cmd.AddCommand(newComposeRenderCmd())
	return cmd
}
