package cli

import (
	"github.com/gcstr/dockform/internal/manifest"
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
