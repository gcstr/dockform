package cli

import (
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

			// Build and display the plan
			plan, err := ctx.BuildPlan()
			if err != nil {
				return err
			}

			out := plan.String()
			ctx.Printer.Plain("%s", out)

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

			// Apply the plan
			if err := ctx.ApplyPlan(); err != nil {
				return err
			}

			// Prune unused resources
			if err := ctx.PrunePlan(); err != nil {
				return err
			}

			return nil
		},
	}
	cmd.Flags().Bool("skip-confirmation", false, "Skip confirmation prompt and apply immediately")
	return cmd
}
