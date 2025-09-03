package cli

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/spf13/cobra"
)

//go:embed template.yml
var dockformTemplate string

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [directory]",
		Short: "Create a template dockform.yml configuration file",
		Long: `Create a template dockform.yml configuration file in the current directory or specified directory.

The generated file contains examples and comments explaining all available configuration options.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Determine target directory
			targetDir := "."
			if len(args) > 0 {
				targetDir = args[0]
			}

			// Check if directory exists
			if targetDir != "." {
				if _, err := os.Stat(targetDir); os.IsNotExist(err) {
					return apperr.New("cli.init", apperr.NotFound, "directory %s does not exist", targetDir)
				} else if err != nil {
					return apperr.Wrap("cli.init", apperr.Internal, err, "check directory %s", targetDir)
				}
			}

			// Create file path
			configPath := filepath.Join(targetDir, "dockform.yml")

			// Check if file already exists
			if _, err := os.Stat(configPath); err == nil {
				return apperr.New("cli.init", apperr.InvalidInput, "dockform.yml already exists in %s", targetDir)
			}

			// Write template to file
			if err := os.WriteFile(configPath, []byte(dockformTemplate), 0644); err != nil {
				return apperr.Wrap("cli.init", apperr.Internal, err, "write dockform.yml")
			}

			// Success message
			relPath := configPath
			if abs, err := filepath.Abs(configPath); err == nil {
				if cwd, err := os.Getwd(); err == nil {
					if rel, err := filepath.Rel(cwd, abs); err == nil && len(rel) < len(abs) {
						relPath = rel
					}
				}
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Created dockform.yml: %s\n", relPath); err != nil {
				return err
			}

			return nil
		},
	}

	return cmd
}
