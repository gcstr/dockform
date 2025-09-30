package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the desired state",
		RunE: func(cmd *cobra.Command, args []string) error {
			skipConfirm, _ := cmd.Flags().GetBool("skip-confirmation")

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

			// Build and display the plan with rolling logs (or direct when verbose)
			planOut, _, err := RunWithRollingOrDirect(cmd, verbose, func(runCtx context.Context) (string, error) {
				prev := ctx.Ctx
				ctx.Ctx = runCtx
				defer func() { ctx.Ctx = prev }()
				plan, err := ctx.BuildPlan()
				if err != nil {
					return "", err
				}
				return plan.String(), nil
			})
			if err != nil {
				return err
			}
			ctx.Printer.Plain("%s", planOut)

			// Get confirmation from user
			confirmed, err := GetConfirmation(cmd, ctx.Printer, ConfirmationOptions{
				SkipConfirmation: skipConfirm,
			})
			if err != nil {
				return err
			}

			if !confirmed {
				return nil
			}

			// Apply + Prune with rolling logs (or direct when verbose)
			_, _, err = RunWithRollingOrDirect(cmd, verbose, func(runCtx context.Context) (string, error) {
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
