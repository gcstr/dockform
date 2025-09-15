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

			// Configure sequential processing if requested (default is parallel)
			sequential, _ := cmd.Flags().GetBool("sequential")
			if sequential {
				ctx.Planner = ctx.Planner.WithParallel(false)
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

	// Add sequential flag
	cmd.Flags().Bool("sequential", false, "Use sequential processing instead of the default parallel processing (slower but uses less CPU and Docker daemon resources)")

	return cmd
}
