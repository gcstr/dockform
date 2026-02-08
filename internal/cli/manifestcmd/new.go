package manifestcmd

import (
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
			pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
			file, err := common.ResolveManifestPath(cmd, pr, ".", 3)
			if err != nil {
				return err
			}
			if file != "" {
				_ = cmd.Flags().Set("manifest", file)
			}

			out, filename, missing, err := manifest.RenderWithWarningsAndPath(file)
			if err != nil {
				return err
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
