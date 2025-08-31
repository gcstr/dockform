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

func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show the plan to reach the desired state",
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("config")
			prune, _ := cmd.Flags().GetBool("prune")

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
			if !prune && strings.Contains(out, "↓ ") {
				pr.Plain("No resources will be removed. Include --prune to delete them")
			}
			return nil
		},
	}
	cmd.Flags().Bool("prune", false, "Show removals that would be applied with --prune")
	return cmd
}
