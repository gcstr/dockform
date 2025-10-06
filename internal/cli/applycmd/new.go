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

			// Build the plan with rolling logs (or direct when verbose)
			var builtPlan *planner.Plan
			verbose, _ := cmd.Flags().GetBool("verbose")
			planOut, usedTUI, err := common.RunWithRollingOrDirect(cmd, verbose, func(runCtx context.Context) (string, error) {
				prev := ctx.Ctx
				ctx.Ctx = runCtx
				defer func() { ctx.Ctx = prev }()
				plan, err := ctx.BuildPlan()
				if err != nil {
					return "", err
				}
				builtPlan = plan
				return plan.String(), nil
			})
			if err != nil {
				return err
			}
			if !usedTUI {
				ctx.Printer.Plain("%s", planOut)
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
			_, _, err = common.RunWithRollingOrDirect(cmd, verbose, func(runCtx context.Context) (string, error) {
				prev := ctx.Ctx
				ctx.Ctx = runCtx
				defer func() { ctx.Ctx = prev }()
				if err := ctx.ApplyPlan(); err != nil {
					return "", err
				}
				if err := ctx.PrunePlan(); err != nil {
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
	return cmd
}
