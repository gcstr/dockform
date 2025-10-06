package plancmd

import (
	"context"

	"github.com/gcstr/dockform/internal/cli/common"
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

			// Configure sequential processing if requested (default is parallel)
			sequential, _ := cmd.Flags().GetBool("sequential")
			if sequential {
				ctx.Planner = ctx.Planner.WithParallel(false)
			}

			// Build plan normally
			verbose, _ := cmd.Flags().GetBool("verbose")
			if verbose {
				plan, err := ctx.BuildPlan()
				if err != nil {
					return err
				}
				ctx.Printer.Plain("%s", plan.String())
			} else {
				var out string
				_, err = ui.RunWithRollingLog(cmd.Context(), func(runCtx context.Context) (string, error) {
					prev := ctx.Ctx
					ctx.Ctx = runCtx
					defer func() { ctx.Ctx = prev }()

					// Check if context is already cancelled before starting
					if runCtx.Err() != nil {
						return "", runCtx.Err()
					}

					plan, err := ctx.BuildPlan()
					if err != nil {
						return "", err
					}

					// Check again after BuildPlan in case it was cancelled during execution
					if runCtx.Err() != nil {
						return "", runCtx.Err()
					}

					out = plan.String()
					return "", nil
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
	cmd.Flags().Bool("sequential", false, "Use sequential processing instead of the default parallel processing (slower but uses less CPU and Docker daemon resources)")

	return cmd
}
