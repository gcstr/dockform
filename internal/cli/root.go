package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func Execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "dockform",
		Short:         "Manage Docker Compose projects declaratively",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringP("config", "c", "", "Path to configuration file or directory (defaults to dockform.yml or dockform.yaml in current directory)")

	cmd.AddCommand(newPlanCmd())
	cmd.AddCommand(newApplyCmd())
	cmd.AddCommand(newValidateCmd())
	cmd.AddCommand(newSecretCmd())
	cmd.AddCommand(newManifestCmd())

	cmd.SetHelpTemplate(cmd.HelpTemplate() + "\n\nProject home: https://github.com/gcstr/dockform\n")

	cmd.SetVersionTemplate(fmt.Sprintf("%s\n", Version()))
	cmd.Version = Version()

	return cmd
}

func Version() string { return "0.1.0-dev" }
