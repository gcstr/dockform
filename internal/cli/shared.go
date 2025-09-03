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

// PlanResult contains all the components needed after building a plan
type PlanResult struct {
	Plan   *planner.Plan
	Config *manifest.Config
	Docker *dockercli.Client
	Printer ui.Printer
}

// buildPlanWithUI handles the common workflow of loading config, validating, and building a plan
// This eliminates duplication between plan and apply commands
func buildPlanWithUI(cmd *cobra.Command) (*PlanResult, error) {
	file, _ := cmd.Flags().GetString("config")
	pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
	
	// Load config and show warnings for missing environment variables
	cfg, missing, err := manifest.LoadWithWarnings(file)
	if err != nil {
		return nil, err
	}
	for _, name := range missing {
		pr.Warn("environment variable %s is not set; replacing with empty string", name)
	}
	
	// Display Docker info section
	displayDockerInfo(pr, &cfg)
	
	// Setup Docker client and validation
	d := dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
	sp := ui.NewSpinner(pr.Out, "Planning...")
	sp.Start()
	
	if err := validator.Validate(context.Background(), cfg, d); err != nil {
		sp.Stop()
		return nil, err
	}
	
	// Build the plan
	pln, err := planner.NewWithDocker(d).WithPrinter(pr).BuildPlan(context.Background(), cfg)
	sp.Stop()
	
	if err != nil {
		return nil, err
	}
	
	return &PlanResult{
		Plan:    pln,
		Config:  &cfg,
		Docker:  d,
		Printer: pr,
	}, nil
}

// displayDockerInfo shows the Docker context and identifier information
func displayDockerInfo(pr ui.Printer, cfg *manifest.Config) {
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
}
