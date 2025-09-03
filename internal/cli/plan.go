package cli

import (
	"github.com/spf13/cobra"
)

func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show the plan to reach the desired state",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Use shared plan building functionality
			result, err := buildPlanWithUI(cmd)
			if err != nil {
				return err
			}
			
			// Display the plan
			out := result.Plan.String()
			result.Printer.Plain("%s", out)
			return nil
		},
	}
	return cmd
}
