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

			pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
			cfg, missing, err := manifest.LoadWithWarnings(file)
			if err != nil {
				return err
			}
			for _, name := range missing {
				pr.Warn("environment variable %s is not set; replacing with empty string", name)
			}
			// Display Docker info section before planning
			ctxName := strings.TrimSpace(cfg.Docker.Context)
			if ctxName == "" {
				ctxName = "default"
			}
			sections := []ui.Section{
				{
					Title: "Docker",
					Items: []ui.DiffLine{
						ui.Line(ui.Info, "Context: %s", ctxName),
						ui.Line(ui.Info, "Identifier: %s", cfg.Docker.Identifier),
					},
				},
			}
			pr.Plain("%s", strings.TrimRight(ui.RenderSectionedList(sections), "\n"))
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
			return nil
		},
	}
	return cmd
}
