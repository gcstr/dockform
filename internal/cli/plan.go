package cli

import (
	"github.com/spf13/cobra"
)

func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show the plan to reach the desired state",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Setup CLI context with all standard initialization
			ctx, err := SetupCLIContext(cmd)
			if err != nil {
				return err
			}
			
			// Build and display the plan
			plan, err := ctx.BuildPlan()
			if err != nil {
				return err
			}
			
			out := plan.String()
			ctx.Printer.Plain("%s", out)
			return nil
		},
	}
	return cmd
}
