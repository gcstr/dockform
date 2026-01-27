package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/gcstr/dockform/internal/validator"
	"github.com/goccy/go-yaml"
	"github.com/mattn/go-isatty"
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

// GetDefaultClient returns a Docker client for the first daemon (for single-daemon operations).
func (ctx *CLIContext) GetDefaultClient() *dockercli.Client {
	for _, daemon := range ctx.Config.Daemons {
		return ctx.Factory.GetClient(daemon.Context, daemon.Identifier)
	}
	return ctx.Factory.GetClient("", "")
}

// LoadConfigWithWarnings loads the configuration from the --config flag and displays warnings.
func LoadConfigWithWarnings(cmd *cobra.Command, pr ui.Printer) (*manifest.Config, error) {
	file, _ := cmd.Flags().GetString("config")
	cfg, missing, err := manifest.LoadWithWarnings(file)
	if err == nil {
		for _, name := range missing {
			pr.Warn("environment variable %s is not set; replacing with empty string", name)
		}
		return &cfg, nil
	}

	// If no config found in CWD and no explicit --config, try interactive discovery
	if file == "" && apperr.IsKind(err, apperr.NotFound) {
		selectedPath, ok, selErr := selectManifestPath(cmd, pr, ".", 3)
		if selErr != nil {
			return nil, selErr
		}
		if ok && selectedPath != "" {
			// Propagate selection to the flag so downstream uses the same path
			_ = cmd.Flags().Set("config", selectedPath)
			cfg2, missing2, err2 := manifest.LoadWithWarnings(selectedPath)
			if err2 != nil {
				return nil, err2
			}
			for _, name := range missing2 {
				pr.Warn("environment variable %s is not set; replacing with empty string", name)
			}
			return &cfg2, nil
		}
	}

	return nil, err
}

// selectManifestPath scans for manifest files up to maxDepth and presents an interactive picker
// of docker.context values when attached to a TTY. Returns the chosen manifest path and whether
// a selection was made. On non-TTY, returns ok=false with no error.
func selectManifestPath(cmd *cobra.Command, pr ui.Printer, root string, maxDepth int) (string, bool, error) {
	// Check TTY
	inTTY := false
	outTTY := false
	if f, ok := cmd.InOrStdin().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		inTTY = true
	}
	if f, ok := cmd.OutOrStdout().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		outTTY = true
	}
	if !inTTY || !outTTY {
		return "", false, nil
	}

	// Discover manifest files
	files, err := findManifestFiles(root, maxDepth)
	if err != nil {
		return "", false, err
	}
	if len(files) == 0 {
		return "", false, nil
	}

	// Build labels by reading docker.context from each file
	labels := make([]string, 0, len(files))
	for _, p := range files {
		lb := readDockerContextLabel(p)
		if strings.TrimSpace(lb) == "" {
			lb = filepath.Base(filepath.Dir(p))
		}
		labels = append(labels, lb)
	}

	// Show picker
	idx, ok, err := ui.SelectOneTTY(cmd.InOrStdin(), cmd.OutOrStdout(), "Target context:", labels)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	if idx < 0 || idx >= len(files) {
		return "", false, nil
	}
	return files[idx], true, nil
}

func findManifestFiles(root string, maxDepth int) ([]string, error) {
	var out []string
	base, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Enforce max depth
		rel, rerr := filepath.Rel(base, path)
		if rerr == nil {
			depth := 0
			if rel != "." {
				depth = strings.Count(rel, string(os.PathSeparator))
			}
			if d.IsDir() && depth > maxDepth {
				return filepath.SkipDir
			}
		}
		if d.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		switch name {
		case "dockform.yml", "dockform.yaml", "Dockform.yml", "Dockform.yaml":
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func readDockerContextLabel(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// Try new multi-daemon schema first
	var tmp struct {
		Daemons map[string]struct {
			Context string `yaml:"context"`
		} `yaml:"daemons"`
	}
	if yerr := yaml.Unmarshal([]byte(b), &tmp); yerr != nil {
		return ""
	}
	// Return comma-separated list of daemon names
	var names []string
	for name := range tmp.Daemons {
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names, ", ")
}

// CreateClientFactory creates a Docker client factory for multi-daemon support.
func CreateClientFactory() *dockercli.DefaultClientFactory {
	return dockercli.NewClientFactory()
}

// CreateDockerClient creates a Docker client for the first daemon in the config.
// Deprecated: Use CreateClientFactory for multi-daemon support.
func CreateDockerClient(cfg *manifest.Config) *dockercli.Client {
	for _, daemon := range cfg.Daemons {
		return dockercli.New(daemon.Context).WithIdentifier(daemon.Identifier)
	}
	return dockercli.New("").WithIdentifier("")
}

// ValidateWithFactory runs validation against the configuration using a client factory.
func ValidateWithFactory(ctx context.Context, cfg *manifest.Config, factory *dockercli.DefaultClientFactory) error {
	return validator.Validate(ctx, *cfg, factory)
}

// ValidateWithDocker runs validation against the configuration and Docker client.
// Deprecated: Use ValidateWithFactory for multi-daemon support.
func ValidateWithDocker(ctx context.Context, cfg *manifest.Config, docker *dockercli.Client) error {
	// Create a temporary factory wrapping this client for backward compatibility
	factory := dockercli.NewClientFactory()
	return validator.Validate(ctx, *cfg, factory)
}

// CreatePlannerWithFactory creates a planner with client factory and printer configured.
func CreatePlannerWithFactory(factory *dockercli.DefaultClientFactory, pr ui.Printer) *planner.Planner {
	return planner.NewWithFactory(factory).WithPrinter(pr)
}

// CreatePlanner creates a planner with Docker client and printer configured.
// Deprecated: Use CreatePlannerWithFactory for multi-daemon support.
func CreatePlanner(docker *dockercli.Client, pr ui.Printer) *planner.Planner {
	return planner.NewWithFactory(dockercli.NewClientFactory()).WithPrinter(pr)
}

// maskSecretsSimple redacts secret-like values from a YAML string based on stack config.
// This is a pragmatic heuristic: it masks occurrences of values provided via stack/environment
// inline env and sops secrets (after decryption via BuildInlineEnv), as well as common sensitive keys.

// SpinnerOperation runs an operation with a spinner, automatically handling start/stop.
func SpinnerOperation(pr ui.StdPrinter, message string, operation func() error) error {
	spinner := ui.NewSpinner(pr.Out, message)
	spinner.Start()
	err := operation()
	spinner.Stop()
	return err
}

// DynamicSpinnerOperation runs an operation with a spinner, passing the spinner to allow dynamic label updates.
func DynamicSpinnerOperation(pr ui.StdPrinter, message string, operation func(*ui.Spinner) error) error {
	spinner := ui.NewSpinner(pr.Out, message)
	spinner.Start()
	err := operation(spinner)
	spinner.Stop()
	return err
}

// RunWithRollingOrDirect executes fn while showing rolling logs when stdout is a TTY and verbose is false.
// Returns the fn's string result and whether the rolling TUI was used.
func RunWithRollingOrDirect(cmd *cobra.Command, verbose bool, fn func(runCtx context.Context) (string, error)) (string, bool, error) {
	// Determine if stdout is a terminal
	useTUI := false
	if f, ok := cmd.OutOrStdout().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		useTUI = true
	}
	if !useTUI || verbose {
		out, err := fn(cmd.Context())
		return out, false, err
	}
	out, err := ui.RunWithRollingLog(cmd.Context(), fn)
	return out, true, err
}

// SetupCLIContext performs the standard CLI setup: load config, create client factory, validate, and create planner.
func SetupCLIContext(cmd *cobra.Command) (*CLIContext, error) {
	pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}

	// Load configuration with warnings
	cfg, err := LoadConfigWithWarnings(cmd, pr)
	if err != nil {
		return nil, err
	}

	// Display daemon info
	displayDockerInfo(pr, cfg)

	// Create client factory for multi-daemon support
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
	var plan *planner.Plan
	var err error

	stdPr := ctx.Printer.(ui.StdPrinter)
	err = SpinnerOperation(stdPr, "Planning...", func() error {
		plan, err = ctx.Planner.BuildPlan(ctx.Ctx, *ctx.Config)
		return err
	})

	return plan, err
}

// ApplyPlan executes the plan with dynamic spinner.
func (ctx *CLIContext) ApplyPlan() error {
	stdPr := ctx.Printer.(ui.StdPrinter)
	return DynamicSpinnerOperation(stdPr, "Applying", func(s *ui.Spinner) error {
		return ctx.Planner.WithSpinner(s, "Applying").Apply(ctx.Ctx, *ctx.Config)
	})
}

// PrunePlan executes pruning with spinner.
func (ctx *CLIContext) PrunePlan() error {
	stdPr := ctx.Printer.(ui.StdPrinter)
	return SpinnerOperation(stdPr, "Pruning...", func() error {
		return ctx.Planner.Prune(ctx.Ctx, *ctx.Config)
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
		plan, err = ctx.Planner.BuildDestroyPlan(ctx.Ctx, *ctx.Config)
		return err
	})

	return plan, err
}

// ExecuteDestroy executes the destruction of all managed resources.
func (ctx *CLIContext) ExecuteDestroy(bgCtx context.Context) error {
	stdPr := ctx.Printer.(ui.StdPrinter)
	return DynamicSpinnerOperation(stdPr, "Destroying", func(s *ui.Spinner) error {
		return ctx.Planner.WithSpinner(s, "Destroying").Destroy(bgCtx, *ctx.Config)
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
