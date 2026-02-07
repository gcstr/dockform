package destroycmd

import (
	"context"
	"os"

	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/spf13/cobra"
)

// New creates the `destroy` command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy all managed resources",
		Long: `Destroy all resources managed by dockform with the configured identifier.

This command will:
- List all containers, networks, volumes, and filesets labeled with the dockform identifier
- Show a plan of what will be destroyed (same format as 'dockform plan')
- Prompt for confirmation by typing the identifier name
- Destroy resources in the correct order (containers → networks → volumes)

Warning: This operation is irreversible and will destroy ALL managed resources,
regardless of what's in your current configuration file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			skipConfirm, _ := cmd.Flags().GetBool("skip-confirmation")

			// Setup CLI context with all standard initialization
			ctx, err := common.SetupCLIContext(cmd)
			if err != nil {
				return err
			}

			// Allow environment to override identifier for discovery/confirmation independence
			identifier := common.GetFirstIdentifier(ctx.Config)
			if override := os.Getenv("DOCKFORM_RUN_ID"); override != "" {
				identifier = override
				// Update project identifier to use the override
				ctx.Config.Identifier = override
			}

			// Build destroy plan using the planner
			plan, err := ctx.BuildDestroyPlan()
			if err != nil {
				return err
			}

			// Display the plan using the same format as 'dockform plan'
			out := plan.String()
			if out == "[no plan]" || out == "" {
				ctx.Printer.Plain("No managed resources found to destroy.")
				return nil
			}

			ctx.Printer.Plain("%s", out)

			// Get confirmation from user (requires typing identifier)
			confirmed, err := common.GetDestroyConfirmation(cmd, ctx.Printer, common.DestroyConfirmationOptions{
				SkipConfirmation: skipConfirm,
				Identifier:       identifier,
			})
			if err != nil {
				return err
			}

			if !confirmed {
				return nil
			}

			// Execute the destruction with rolling logs (or direct when verbose)
			verbose, _ := cmd.Flags().GetBool("verbose")
			strict, _ := cmd.Flags().GetBool("strict")
			verboseErrors, _ := cmd.Flags().GetBool("verbose-errors")
			_, _, err = common.RunWithRollingOrDirect(cmd, verbose, func(runCtx context.Context) (string, error) {
				prev := ctx.Ctx
				ctx.Ctx = runCtx
				defer func() { ctx.Ctx = prev }()
				if err := ctx.ExecuteDestroyWithOptions(runCtx, planner.CleanupOptions{
					Strict:        strict,
					VerboseErrors: verboseErrors,
				}); err != nil {
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
	cmd.Flags().Bool("skip-confirmation", false, "Skip confirmation prompt and destroy immediately")
	cmd.Flags().Bool("strict", false, "Fail destroy when cleanup operations encounter errors")
	cmd.Flags().Bool("verbose-errors", false, "Print detailed cleanup error details when not using --strict")
	common.AddTargetFlags(cmd)
	return cmd
}
