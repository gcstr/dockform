package cli

import (
	"context"
	"strings"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/gcstr/dockform/internal/validator"
	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the desired state",
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("config")
			pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
			cfg, missing, err := manifest.LoadWithWarnings(file)
			if err != nil {
				return err
			}
			for _, name := range missing {
				pr.Warn("environment variable %s is not set; replacing with empty string", name)
			}
			prune, _ := cmd.Flags().GetBool("prune")

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
			// Print guidance only when removals are present and --prune not set
			if !prune && strings.Contains(out, "↓ ") {
				pr.Plain("No resources will be removed. Include --prune to delete them")
			}

			// Skip Apply when there are no add/change operations and no filesets configured
			noAdds := !strings.Contains(out, "↑ ")
			noChanges := !strings.Contains(out, "→ ")
			if noAdds && noChanges && len(cfg.Filesets) == 0 {
				return nil
			}

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

			if prune {
				sp3 := ui.NewSpinner(pr.Out, "Pruning...")
				sp3.Start()
				if err := planner.NewWithDocker(d).WithPrinter(pr).Prune(context.Background(), cfg); err != nil {
					sp3.Stop()
					return err
				}
				sp3.Stop()
			}
			return nil
		},
	}
	cmd.Flags().Bool("prune", false, "Delete unmanaged resources after confirmation")
	return cmd
}
