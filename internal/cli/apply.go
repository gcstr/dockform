package cli

import (
	"bufio"
	"context"
	"os"
	"strings"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/gcstr/dockform/internal/validator"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the desired state",
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("config")
			skipConfirm, _ := cmd.Flags().GetBool("skip-confirmation")
			pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
			cfg, missing, err := manifest.LoadWithWarnings(file)
			if err != nil {
				return err
			}
			for _, name := range missing {
				pr.Warn("environment variable %s is not set; replacing with empty string", name)
			}

			d := dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
			sp := ui.NewSpinner(pr.Out, "Planning...")
			sp.Start()
			if err := validator.Validate(context.Background(), cfg, d); err != nil {
				sp.Stop()
				return err
			}
			pln, err := planner.NewWithDocker(d).WithPrinter(pr).BuildPlan(context.Background(), cfg)
			if err != nil {
				sp.Stop()
				return err
			}
			sp.Stop()
			out := pln.String()
			pr.Plain("%s", out)

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
					pr.Plain("Answer: %s", entered)
					pr.Plain("")
				} else {
					// Non-interactive: fall back to plain stdin read (keeps tests/scriptability)
					pr.Plain("Dockform will apply the changes listed above.\nType yes to confirm.\n\nAnswer")
					reader := bufio.NewReader(cmd.InOrStdin())
					ans, _ := reader.ReadString('\n')
					entered := strings.TrimRight(ans, "\n")
					confirmed = strings.TrimSpace(entered) == "yes"
					// Echo user input only when stdin isn't a TTY (interactive terminals already echo)
					if f, ok := cmd.InOrStdin().(*os.File); !ok || !isatty.IsTerminal(f.Fd()) {
						pr.Plain("%s", entered)
					}
					pr.Plain("")
				}
			}

			if !confirmed {
				pr.Plain("canceled")
				return nil
			}

			// Always run apply tasks; do not skip based on plan output

			sp2 := ui.NewSpinner(pr.Out, "")
			sp2.Start()
			pb := ui.NewProgress(pr.Out, "Applying")
			if err := planner.NewWithDocker(d).WithPrinter(pr).WithProgress(pb).Apply(context.Background(), cfg); err != nil {
				sp2.Stop()
				pb.Stop()
				return err
			}
			sp2.Stop()
			pb.Stop()

			sp3 := ui.NewSpinner(pr.Out, "Pruning...")
			sp3.Start()
			if err := planner.NewWithDocker(d).WithPrinter(pr).Prune(context.Background(), cfg); err != nil {
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
