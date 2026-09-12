package applycmd

import (
	"context"
	"os"

	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/runlog"
	"github.com/gcstr/dockform/internal/ui/applyview"
	"github.com/mattn/go-isatty"
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

			// Always write a full run log: after a failure there is no second
			// chance to ask for one. Fan it into the context logger so
			// everything apply logs — BuildPlan below included — lands in the
			// file, on top of whatever the console sink is already doing.
			rl, rlErr := runlog.Open(ctx.Config.BaseDir)
			runLogPath := ""
			if rlErr != nil {
				ctx.Printer.Warn("could not open a run log: %v", rlErr)
			} else {
				defer func() { _ = rl.Close() }()
				runLogPath = rl.Path()
				if w := rl.Warning(); w != "" {
					ctx.Printer.Warn("%s", w)
				}
				ctx.Ctx = logger.WithContext(ctx.Ctx, logger.Fanout(logger.FromContext(ctx.Ctx), rl.Logger()))
				// cmd.Context() is what every RunWithRollingOrDirect/RunOrPlain
				// call below threads through as runCtx, and WithRunContext then
				// swaps it into ctx.Ctx for the call's duration — so the fan-out
				// above only takes effect once it is also the command's own
				// context, not just this CLIContext's copy of it.
				cmd.SetContext(ctx.Ctx)
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
			long, _ := cmd.Flags().GetBool("long")

			// If the plan has no create/update/delete actions, inform and exit early
			// (before the review render, so we don't print both "No changes…" and this).
			if builtPlan != nil && builtPlan.Resources != nil {
				createCount, updateCount, deleteCount := builtPlan.Resources.CountActions()
				if createCount == 0 && updateCount == 0 && deleteCount == 0 {
					ctx.Printer.Plain("Nothing to apply. Exiting.")
					return nil
				}
			}

			// Print the plan for review. Goes through the normal printer so it
			// scrolls naturally instead of being clipped by the rolling-log TUI.
			// --long shows all resources including no-ops; default is changes-only.
			if builtPlan != nil {
				ctx.Printer.Plain("%s", builtPlan.Render(planner.PlanRenderOptions{Full: long}))
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

			// Apply + Prune, showing the per-resource status view inline (or the
			// plain non-interactive path when stdout isn't a terminal, or
			// --verbose was passed). Mirrors RunWithRollingOrDirect's own TTY
			// check, since this no longer goes through it.
			useTUI := false
			if f, ok := cmd.OutOrStdout().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
				useTUI = true
			}
			plain := !useTUI || verbose

			strictPrune, _ := cmd.Flags().GetBool("strict-prune")
			verbosePruneErrors, _ := cmd.Flags().GetBool("verbose-prune-errors")
			err = applyview.RunOrPlain(cmd.Context(), plain, runLogPath, func(runCtx context.Context, reporter planner.ProgressReporter) error {
				return ctx.WithRunContext(runCtx, func() error {
					// Pass the pre-built plan to avoid redundant state detection
					if err := ctx.ApplyPlanWithReporter(builtPlan, reporter); err != nil {
						return err
					}
					// Also pass the plan to prune to reuse execution context
					return ctx.PrunePlanWithOptions(builtPlan, planner.CleanupOptions{
						Strict:        strictPrune,
						VerboseErrors: verbosePruneErrors,
					})
				})
			})
			if err != nil {
				return err
			}

			return nil
		},
	}
	cmd.Flags().Bool("skip-confirmation", false, "Skip confirmation prompt and apply immediately")
	cmd.Flags().Int("parallel", common.DefaultParallel, "How many docker operations to run at once against each remote host. Lower it for small servers; 1 runs everything one at a time. It does not limit how many containers compose starts at once within a single stack")
	cmd.Flags().Bool("sequential", false, "Deprecated: use --parallel 1")
	_ = cmd.Flags().MarkDeprecated("sequential", "use --parallel 1 instead")
	cmd.Flags().Bool("long", false, "Show the full plan including unchanged resources")
	cmd.Flags().Bool("strict-prune", false, "Fail apply when prune operations encounter errors")
	cmd.Flags().Bool("verbose-prune-errors", false, "Print detailed prune error details when not using --strict-prune")
	common.AddTargetFlags(cmd)
	return cmd
}
