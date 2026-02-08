package common

import (
	"context"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

// CLIContext contains all the components needed for most CLI operations.
type CLIContext struct {
	Ctx     context.Context
	Config  *manifest.Config
	Factory *dockercli.DefaultClientFactory
	Printer ui.Printer
	Planner *planner.Planner
}

// WithRunContext temporarily swaps the context's Ctx to runCtx for the
// duration of fn, restoring the original afterwards.
func (c *CLIContext) WithRunContext(runCtx context.Context, fn func() error) error {
	prev := c.Ctx
	c.Ctx = runCtx
	defer func() { c.Ctx = prev }()
	return fn()
}

// GetDefaultClient returns a Docker client for the first context (for single-context operations).
func (ctx *CLIContext) GetDefaultClient() *dockercli.Client {
	name, _ := GetFirstDaemon(ctx.Config)
	return ctx.Factory.GetClient(name, ctx.Config.Identifier)
}

// SetupCLIContext performs the standard CLI setup: load config, create client factory, validate, and create planner.
func SetupCLIContext(cmd *cobra.Command) (*CLIContext, error) {
	pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}

	// Load configuration with warnings
	cfg, err := LoadConfigWithWarnings(cmd, pr)
	if err != nil {
		return nil, err
	}

	// Apply target filtering if flags are registered
	if cmd.Flags().Lookup("deployment") != nil {
		opts := ReadTargetOptions(cmd)
		if !opts.IsEmpty() {
			cfg, err = ResolveTargets(cfg, opts)
			if err != nil {
				return nil, err
			}
		}
	}

	// Display context info
	DisplayDaemonInfo(pr, cfg)

	// Create client factory for multi-context support
	factory := CreateClientFactory()

	// Validate in spinner
	err = SpinnerOperation(pr, "Validating...", func() error {
		return ValidateWithFactory(cmd.Context(), cfg, factory)
	})
	if err != nil {
		return nil, err
	}

	// Create planner with factory
	plan := CreatePlannerWithFactory(factory, pr)

	return &CLIContext{
		Ctx:     cmd.Context(),
		Config:  cfg,
		Factory: factory,
		Printer: pr,
		Planner: plan,
	}, nil
}

// BuildPlan creates a plan using the CLI context with spinner UI.
func (ctx *CLIContext) BuildPlan() (*planner.Plan, error) {
	var planObj *planner.Plan
	var err error

	stdPr := ctx.Printer.(ui.StdPrinter)
	err = SpinnerOperation(stdPr, "Planning...", func() error {
		planObj, err = ctx.Planner.BuildPlan(ctx.Ctx, *ctx.Config)
		return err
	})

	return planObj, err
}

// ApplyPlan executes the plan with dynamic spinner.
func (ctx *CLIContext) ApplyPlan() error {
	stdPr := ctx.Printer.(ui.StdPrinter)
	return DynamicSpinnerOperation(stdPr, "Applying", func(s *ui.Spinner) error {
		return ctx.Planner.WithSpinner(s, "Applying").Apply(ctx.Ctx, *ctx.Config)
	})
}

// ApplyPlanWithContext executes the plan with progress tracking, reusing a pre-built plan.
// This avoids redundant state detection by passing the execution context from the plan.
func (ctx *CLIContext) ApplyPlanWithContext(plan *planner.Plan) error {
	stdPr := ctx.Printer.(ui.StdPrinter)
	return DynamicSpinnerOperation(stdPr, "Applying", func(s *ui.Spinner) error {
		return ctx.Planner.WithSpinner(s, "Applying").ApplyWithPlan(ctx.Ctx, *ctx.Config, plan)
	})
}

// PrunePlan executes pruning with spinner.
func (ctx *CLIContext) PrunePlan() error {
	stdPr := ctx.Printer.(ui.StdPrinter)
	return SpinnerOperation(stdPr, "Pruning...", func() error {
		return ctx.Planner.Prune(ctx.Ctx, *ctx.Config)
	})
}

// PrunePlanWithContext executes pruning with spinner, reusing a pre-built plan.
func (ctx *CLIContext) PrunePlanWithContext(plan *planner.Plan) error {
	return ctx.PrunePlanWithOptions(plan, planner.CleanupOptions{Strict: true, VerboseErrors: true})
}

// PrunePlanWithOptions executes pruning with spinner, reusing a pre-built plan and explicit cleanup options.
func (ctx *CLIContext) PrunePlanWithOptions(plan *planner.Plan, opts planner.CleanupOptions) error {
	stdPr := ctx.Printer.(ui.StdPrinter)
	return SpinnerOperation(stdPr, "Pruning...", func() error {
		return ctx.Planner.PruneWithPlanOptions(ctx.Ctx, *ctx.Config, plan, opts)
	})
}

// BuildDestroyPlan creates a destruction plan for all managed resources.
func (ctx *CLIContext) BuildDestroyPlan() (*planner.Plan, error) {
	var planObj *planner.Plan
	var err error

	stdPr := ctx.Printer.(ui.StdPrinter)
	err = SpinnerOperation(stdPr, "Discovering resources...", func() error {
		planObj, err = ctx.Planner.BuildDestroyPlan(ctx.Ctx, *ctx.Config)
		return err
	})

	return planObj, err
}

// ExecuteDestroy executes the destruction of all managed resources.
func (ctx *CLIContext) ExecuteDestroy(bgCtx context.Context) error {
	return ctx.ExecuteDestroyWithOptions(bgCtx, planner.CleanupOptions{Strict: true, VerboseErrors: true})
}

// ExecuteDestroyWithOptions executes destruction with explicit cleanup options.
func (ctx *CLIContext) ExecuteDestroyWithOptions(bgCtx context.Context, opts planner.CleanupOptions) error {
	stdPr := ctx.Printer.(ui.StdPrinter)
	return DynamicSpinnerOperation(stdPr, "Destroying", func(s *ui.Spinner) error {
		return ctx.Planner.WithSpinner(s, "Destroying").DestroyWithOptions(bgCtx, *ctx.Config, opts)
	})
}
