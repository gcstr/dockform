package cli

import (
	"bufio"
	"context"
	"fmt"
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

// CLIContext contains all the components needed for most CLI operations.
type CLIContext struct {
	Config  *manifest.Config
	Docker  *dockercli.Client
	Printer ui.Printer
	Planner *planner.Planner
}

// LoadConfigWithWarnings loads the configuration from the --config flag and displays warnings.
func LoadConfigWithWarnings(cmd *cobra.Command, pr ui.Printer) (*manifest.Config, error) {
	file, _ := cmd.Flags().GetString("config")
	cfg, missing, err := manifest.LoadWithWarnings(file)
	if err != nil {
		return nil, err
	}

	// Display warnings for missing environment variables
	for _, name := range missing {
		pr.Warn("environment variable %s is not set; replacing with empty string", name)
	}

	return &cfg, nil
}

// CreateDockerClient creates a Docker client using config context and identifier.
func CreateDockerClient(cfg *manifest.Config) *dockercli.Client {
	return dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
}

// ValidateWithDocker runs validation against the configuration and Docker client.
func ValidateWithDocker(ctx context.Context, cfg *manifest.Config, docker *dockercli.Client) error {
	return validator.Validate(ctx, *cfg, docker)
}

// CreatePlanner creates a planner with Docker client and printer configured.
func CreatePlanner(docker *dockercli.Client, pr ui.Printer) *planner.Planner {
	return planner.NewWithDocker(docker).WithPrinter(pr)
}

// SpinnerOperation runs an operation with a spinner, automatically handling start/stop.
func SpinnerOperation(pr ui.StdPrinter, message string, operation func() error) error {
	spinner := ui.NewSpinner(pr.Out, message)
	spinner.Start()
	err := operation()
	spinner.Stop()
	return err
}

// ProgressOperation runs an operation with a progress bar.
func ProgressOperation(pr ui.StdPrinter, message string, operation func(*ui.Progress) error) error {
	pb := ui.NewProgress(pr.Out, message)
	err := operation(pb)
	pb.Stop()
	return err
}

// SetupCLIContext performs the standard CLI setup: load config, create Docker client, validate, and create planner.
func SetupCLIContext(cmd *cobra.Command) (*CLIContext, error) {
	pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}

	// Load configuration with warnings
	cfg, err := LoadConfigWithWarnings(cmd, pr)
	if err != nil {
		return nil, err
	}

	// Display Docker info
	displayDockerInfo(pr, cfg)

	// Create Docker client
	docker := CreateDockerClient(cfg)

	// Validate in spinner
	err = SpinnerOperation(pr, "Validating...", func() error {
		return ValidateWithDocker(context.Background(), cfg, docker)
	})
	if err != nil {
		return nil, err
	}

	// Create planner
	planner := CreatePlanner(docker, pr)

	return &CLIContext{
		Config:  cfg,
		Docker:  docker,
		Printer: pr,
		Planner: planner,
	}, nil
}

// BuildPlan creates a plan using the CLI context with spinner UI.
func (ctx *CLIContext) BuildPlan() (*planner.Plan, error) {
	var plan *planner.Plan
	var err error

	stdPr := ctx.Printer.(ui.StdPrinter)
	err = SpinnerOperation(stdPr, "Planning...", func() error {
		plan, err = ctx.Planner.BuildPlan(context.Background(), *ctx.Config)
		return err
	})

	return plan, err
}

// ApplyPlan executes the plan with progress tracking.
func (ctx *CLIContext) ApplyPlan() error {
	stdPr := ctx.Printer.(ui.StdPrinter)
	return ProgressOperation(stdPr, "Applying", func(pb *ui.Progress) error {
		return ctx.Planner.WithProgress(pb).Apply(context.Background(), *ctx.Config)
	})
}

// PrunePlan executes pruning with spinner.
func (ctx *CLIContext) PrunePlan() error {
	stdPr := ctx.Printer.(ui.StdPrinter)
	return SpinnerOperation(stdPr, "Pruning...", func() error {
		return ctx.Planner.Prune(context.Background(), *ctx.Config)
	})
}

// ConfirmationOptions configures the confirmation prompt behavior.
type ConfirmationOptions struct {
	SkipConfirmation bool
	Message          string
}

// GetConfirmation handles user confirmation with TTY detection and appropriate prompting.
func GetConfirmation(cmd *cobra.Command, pr ui.Printer, opts ConfirmationOptions) (bool, error) {
	if opts.SkipConfirmation {
		return true, nil
	}

	if opts.Message == "" {
		opts.Message = "│ Dockform will apply the changes listed above.\n│ Type yes to confirm.\n│"
	}

	// Check TTY status
	inTTY := false
	outTTY := false
	if f, ok := cmd.InOrStdin().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		inTTY = true
	}
	if f, ok := cmd.OutOrStdout().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		outTTY = true
	}

	if inTTY && outTTY {
		// Interactive terminal: use Bubble Tea prompt which renders headers and input
		ok, _, err := ui.ConfirmYesTTY(cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil {
			return false, err
		}
		if ok {
			pr.Plain("│ %s", ui.SuccessMark())
			pr.Plain("")
			return true, nil
		}
		pr.Plain("│ %s", ui.RedText("canceled"))
		pr.Plain("")
		return false, nil
	}

	// Non-interactive: fall back to plain stdin read with bordered lines
	pr.Plain("%s\n│ Answer", opts.Message)
	reader := bufio.NewReader(cmd.InOrStdin())
	ans, _ := reader.ReadString('\n')
	entered := strings.TrimRight(ans, "\n")
	confirmed := strings.TrimSpace(entered) == "yes"

	// Echo user input only when stdin isn't a TTY
	if f, ok := cmd.InOrStdin().(*os.File); !ok || !isatty.IsTerminal(f.Fd()) {
		pr.Plain("%s", entered)
	}

	if confirmed {
		pr.Plain("│ %s", ui.SuccessMark())
		pr.Plain("")
		return true, nil
	}

	pr.Plain("│ %s", ui.RedText("canceled"))
	pr.Plain("")
	return false, nil
}

// BuildDestroyPlan creates a destruction plan for all managed resources.
func (ctx *CLIContext) BuildDestroyPlan() (*planner.Plan, error) {
	var plan *planner.Plan
	var err error

	stdPr := ctx.Printer.(ui.StdPrinter)
	err = SpinnerOperation(stdPr, "Discovering resources...", func() error {
		plan, err = ctx.Planner.BuildDestroyPlan(context.Background(), *ctx.Config)
		return err
	})

	return plan, err
}

// ExecuteDestroy executes the destruction of all managed resources.
func (ctx *CLIContext) ExecuteDestroy(bgCtx context.Context) error {
	stdPr := ctx.Printer.(ui.StdPrinter)
	return ProgressOperation(stdPr, "Destroying", func(pb *ui.Progress) error {
		return ctx.Planner.WithProgress(pb).Destroy(bgCtx, *ctx.Config)
	})
}

// DestroyConfirmationOptions configures the destroy confirmation prompt behavior.
type DestroyConfirmationOptions struct {
	SkipConfirmation bool
	Identifier       string
}

// GetDestroyConfirmation handles user confirmation for destroy operations,
// requiring the user to type the identifier name.
func GetDestroyConfirmation(cmd *cobra.Command, pr ui.Printer, opts DestroyConfirmationOptions) (bool, error) {
	if opts.SkipConfirmation {
		return true, nil
	}

	msgSummary := fmt.Sprintf("│ This will destroy ALL managed resources with identifier '%s'.\n│ This operation is IRREVERSIBLE.", opts.Identifier)
	msgInstr := fmt.Sprintf("│ Type the identifier name '%s' to confirm.\n│", ui.ConfirmToken(opts.Identifier))

	// Check TTY status
	inTTY := false
	outTTY := false
	if f, ok := cmd.InOrStdin().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		inTTY = true
	}
	if f, ok := cmd.OutOrStdout().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		outTTY = true
	}

	if inTTY && outTTY {
		// Interactive terminal: Bubble Tea prompt renders the view; we just show result line after
		ok, _, err := ui.ConfirmIdentifierTTY(cmd.InOrStdin(), cmd.OutOrStdout(), opts.Identifier)
		if err != nil {
			return false, err
		}
		if ok {
			pr.Plain("│ %s", ui.SuccessMark())
			pr.Plain("")
			return true, nil
		}
		pr.Plain("│ %s", ui.RedText("canceled"))
		pr.Plain("")
		return false, nil
	}

	// Non-interactive: show bordered lines and read from stdin
	pr.Plain("%s\n│\n%s\n│\n│ Answer", msgSummary, msgInstr)
	reader := bufio.NewReader(cmd.InOrStdin())
	ans, _ := reader.ReadString('\n')
	entered := strings.TrimSpace(ans)
	confirmed := entered == opts.Identifier

	// Echo user input only when stdin isn't a TTY
	if f, ok := cmd.InOrStdin().(*os.File); !ok || !isatty.IsTerminal(f.Fd()) {
		pr.Plain("%s", entered)
	}

	if confirmed {
		pr.Plain("│ %s", ui.SuccessMark())
		pr.Plain("")
		return true, nil
	}

	pr.Plain("│ %s", ui.RedText("canceled"))
	pr.Plain("")
	return false, nil
}
