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
			
			// Configure parallel processing if requested
			parallel, _ := cmd.Flags().GetBool("parallel")
			if parallel {
				ctx.Planner = ctx.Planner.WithParallel(true)
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
	
	// Add parallel flag
	cmd.Flags().Bool("parallel", false, "Enable parallel processing for faster planning (uses more CPU and Docker daemon resources)")
	
	return cmd
}
