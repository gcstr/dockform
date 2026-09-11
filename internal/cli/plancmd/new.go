package plancmd

import (
	"context"

	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

// New creates the `plan` command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show the plan to reach the desired state",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Setup CLI context with all standard initialization
			ctx, err := common.SetupCLIContext(cmd)
			if err != nil {
				return err
			}

			long, _ := cmd.Flags().GetBool("long")
			renderOpts := planner.PlanRenderOptions{Full: long}

			// Build plan normally
			verbose, _ := cmd.Flags().GetBool("verbose")
			if verbose {
				plan, err := ctx.BuildPlan()
				if err != nil {
					return err
				}
				ctx.Printer.Plain("%s", plan.Render(renderOpts))
			} else {
				var out string
				_, err = ui.RunWithRollingLog(cmd.Context(), func(runCtx context.Context) (string, error) {
					return "", ctx.WithRunContext(runCtx, func() error {
						// Check if context is already cancelled before starting
						if runCtx.Err() != nil {
							return runCtx.Err()
						}

						plan, err := ctx.BuildPlan()
						if err != nil {
							return err
						}

						// Check again after BuildPlan in case it was cancelled during execution
						if runCtx.Err() != nil {
							return runCtx.Err()
						}

						out = plan.Render(renderOpts)
						return nil
					})
				})
				if err != nil {
					return err
				}
				ctx.Printer.Plain("%s", out)
			}
			return nil
		},
	}

	// Add sequential flag
	cmd.Flags().Int("parallel", common.DefaultParallel, "How many docker operations to run at once against each remote host. Lower it for small servers; 1 runs everything one at a time. It does not limit how many containers compose starts at once within a single stack")
	cmd.Flags().Bool("sequential", false, "Deprecated: use --parallel 1")
	_ = cmd.Flags().MarkDeprecated("sequential", "use --parallel 1 instead")

	// Add long flag
	cmd.Flags().Bool("long", false, "Show the full plan including unchanged resources")

	// Add targeting flags
	common.AddTargetFlags(cmd)

	return cmd
}
