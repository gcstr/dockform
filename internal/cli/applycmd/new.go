package applycmd

import (
	"context"

	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/spf13/cobra"
)

// New creates the `apply` command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the desired state",
		RunE: func(cmd *cobra.Command, args []string) error {
			skipConfirm, _ := cmd.Flags().GetBool("skip-confirmation")

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

			// Build the plan with rolling logs (or direct when verbose). The rolling
			// log shows BuildPlan progress only — we deliberately do not hand it the
			// plan as its final report, because the TUI renders inline and clips a
			// tall plan to the terminal height, hiding creates/destroys before the
			// confirm prompt (dockform-ltv). The full plan is printed below instead.
			var builtPlan *planner.Plan
			verbose, _ := cmd.Flags().GetBool("verbose")
			_, _, err = common.RunWithRollingOrDirect(cmd, verbose, func(runCtx context.Context) (string, error) {
				return "", ctx.WithRunContext(runCtx, func() error {
					plan, err := ctx.BuildPlan()
					if err != nil {
						return err
					}
					builtPlan = plan
					return nil
				})
			})
			if err != nil {
				return err
			}
			// Print the full plan for review. Goes through the normal printer so it
			// scrolls naturally instead of being clipped by the rolling-log TUI.
			if builtPlan != nil {
				ctx.Printer.Plain("%s", builtPlan.String())
			}

			// If the plan has no create/update/delete actions, inform and exit early
			if builtPlan != nil && builtPlan.Resources != nil {
				createCount, updateCount, deleteCount := builtPlan.Resources.CountActions()
				if createCount == 0 && updateCount == 0 && deleteCount == 0 {
					ctx.Printer.Plain("Nothing to apply. Exiting.")
					return nil
				}
			}

			// Get confirmation from user
			confirmed, err := common.GetConfirmation(cmd, ctx.Printer, common.ConfirmationOptions{
				SkipConfirmation: skipConfirm,
				Message:          "",
			})
			if err != nil {
				return err
			}

			if !confirmed {
				return nil
			}

			// Apply + Prune with rolling logs (or direct when verbose)
			strictPrune, _ := cmd.Flags().GetBool("strict-prune")
			verbosePruneErrors, _ := cmd.Flags().GetBool("verbose-prune-errors")
			_, _, err = common.RunWithRollingOrDirect(cmd, verbose, func(runCtx context.Context) (string, error) {
				err := ctx.WithRunContext(runCtx, func() error {
					// Pass the pre-built plan to avoid redundant state detection
					if err := ctx.ApplyPlanWithContext(builtPlan); err != nil {
						return err
					}
					// Also pass the plan to prune to reuse execution context
					return ctx.PrunePlanWithOptions(builtPlan, planner.CleanupOptions{
						Strict:        strictPrune,
						VerboseErrors: verbosePruneErrors,
					})
				})
				if err != nil {
					return "", err
				}
				return "│ Done.", nil
			})
			if err != nil {
				return err
			}

			return nil
		},
	}
	cmd.Flags().Bool("skip-confirmation", false, "Skip confirmation prompt and apply immediately")
	cmd.Flags().Bool("sequential", false, "Use sequential processing instead of the default parallel processing (slower but uses less CPU and Docker daemon resources)")
	cmd.Flags().Bool("strict-prune", false, "Fail apply when prune operations encounter errors")
	cmd.Flags().Bool("verbose-prune-errors", false, "Print detailed prune error details when not using --strict-prune")
	common.AddTargetFlags(cmd)
	return cmd
}
