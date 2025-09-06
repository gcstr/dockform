package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration and environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Setup CLI context (which includes validation)
			_, err := SetupCLIContext(cmd)
			if err != nil {
				return err
			}
			
			// If we get here, validation was successful
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "validation successful"); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}
