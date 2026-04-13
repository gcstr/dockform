package imagescmd

import "github.com/spf13/cobra"

// New returns the "images" parent command with subcommands.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "images",
		Short: "Manage and check container images",
	}
	cmd.AddCommand(newCheckCmd())
	return cmd
}
