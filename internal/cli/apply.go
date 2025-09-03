package cli

import (
	"bufio"
	"context"
	"os"
	"strings"

	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the desired state",
		RunE: func(cmd *cobra.Command, args []string) error {
			skipConfirm, _ := cmd.Flags().GetBool("skip-confirmation")
			
			// Use shared plan building functionality
			result, err := buildPlanWithUI(cmd)
			if err != nil {
				return err
			}
			
			// Display the plan
			out := result.Plan.String()
			result.Printer.Plain("%s", out)

			// Confirmation prompt before applying changes
			confirmed := false
			if skipConfirm {
				confirmed = true
			} else {
				inTTY := false
				outTTY := false
				if f, ok := cmd.InOrStdin().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
					inTTY = true
				}
				if f, ok := cmd.OutOrStdout().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
					outTTY = true
				}

				if inTTY && outTTY {
					// Interactive terminal: use Bubble Tea prompt
					ok, entered, err := ui.ConfirmYesTTY(cmd.InOrStdin(), cmd.OutOrStdout())
					if err != nil {
						return err
					}
					confirmed = ok
					// Only echo the final input line to avoid duplicating the header prompt.
					result.Printer.Plain(" Answer: %s", entered)
					result.Printer.Plain("")
				} else {
					// Non-interactive: fall back to plain stdin read (keeps tests/scriptability)
					result.Printer.Plain("Dockform will apply the changes listed above.\nType yes to confirm.\n\nAnswer")
					reader := bufio.NewReader(cmd.InOrStdin())
					ans, _ := reader.ReadString('\n')
					entered := strings.TrimRight(ans, "\n")
					confirmed = strings.TrimSpace(entered) == "yes"
					// Echo user input only when stdin isn't a TTY (interactive terminals already echo)
					if f, ok := cmd.InOrStdin().(*os.File); !ok || !isatty.IsTerminal(f.Fd()) {
						result.Printer.Plain("%s", entered)
					}
					result.Printer.Plain("")
				}
			}

			if !confirmed {
				result.Printer.Plain(" canceled")
				return nil
			}

			// Always run apply tasks; do not skip based on plan output

			stdPr := result.Printer.(ui.StdPrinter)
			sp2 := ui.NewSpinner(stdPr.Out, "")
			sp2.Start()
			pb := ui.NewProgress(stdPr.Out, "Applying")
			if err := planner.NewWithDocker(result.Docker).WithPrinter(result.Printer).WithProgress(pb).Apply(context.Background(), *result.Config); err != nil {
				sp2.Stop()
				pb.Stop()
				return err
			}
			sp2.Stop()
			pb.Stop()

			sp3 := ui.NewSpinner(stdPr.Out, "Pruning...")
			sp3.Start()
			if err := planner.NewWithDocker(result.Docker).WithPrinter(result.Printer).Prune(context.Background(), *result.Config); err != nil {
				sp3.Stop()
				return err
			}
			sp3.Stop()
			return nil
		},
	}
	cmd.Flags().Bool("skip-confirmation", false, "Skip confirmation prompt and apply immediately")
	return cmd
}
