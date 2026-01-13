package manifestcmd

import (
	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

// New creates the top-level `manifest` command and wires subcommands
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Work with the manifest file",
	}

	cmd.AddCommand(newRenderCmd())
	return cmd
}

func newRenderCmd() *cobra.Command {
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
					if selPath, ok, selErr := common.SelectManifestPath(cmd, pr, ".", 3, ""); selErr == nil && ok {
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
