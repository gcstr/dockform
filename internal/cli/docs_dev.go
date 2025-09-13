//go:build dev

package cli

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// registerDocsCmd adds the docs command in dev builds.
func registerDocsCmd(root *cobra.Command) {
	root.AddCommand(newDocsCmd())
}

// newDocsCmd creates a hidden command that generates CLI docs from the Cobra tree.
func newDocsCmd() *cobra.Command {
	var outDir string

	cmd := &cobra.Command{
		Use:    "docs",
		Short:  "Generate CLI documentation (Markdown tree)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if outDir == "" {
				outDir = "docs/cli"
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return err
			}

			root := cmd.Root()
			root.DisableAutoGenTag = true
			return doc.GenMarkdownTree(root, outDir)
		},
	}

	cmd.Flags().StringVarP(&outDir, "out", "o", "docs/cli", "Output directory for generated docs")

	return cmd
}
