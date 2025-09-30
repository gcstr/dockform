package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	Docker  *dockercli.Client
	Printer ui.Printer
	Planner *planner.Planner
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
	var tmp struct {
		Docker struct {
			Context string `yaml:"context"`
		} `yaml:"docker"`
	}
	if yerr := yaml.Unmarshal([]byte(b), &tmp); yerr != nil {
		return ""
	}
	return tmp.Docker.Context
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

// maskSecretsSimple redacts secret-like values from a YAML string based on application config.
// This is a pragmatic heuristic: it masks occurrences of values provided via app/environment
// inline env and sops secrets (after decryption via BuildInlineEnv), as well as common sensitive keys.
func maskSecretsSimple(yaml string, app manifest.Application, strategy maskStrategy) string {
	// Determine mask replacement based on strategy
	mask := func(s string) string {
		switch strategy {
		case maskPartial:
			if len(s) <= 4 {
				return "****"
			}
			return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
		case maskPreserveLength:
			if l := len(s); l > 0 {
				return strings.Repeat("*", l)
			}
			return ""
		case maskFull:
			fallthrough
		default:
			return "********"
		}
	}

	// Mask by common sensitive keys patterns: password, secret, token, key
	// YAML format allows: key: value or key: "value"
	// We keep it simple and mask the value part.
	keyPatterns := []string{"password", "secret", "token", "key", "apikey", "api_key", "access_key", "private_key"}
	for _, kp := range keyPatterns {
		// (?i) case-insensitive; match lines like "kp: something"
		re := regexp.MustCompile(`(?i)(` + kp + `\s*:\s*)([^\n#]+)`) // stop at newline or comment
		yaml = re.ReplaceAllStringFunc(yaml, func(m string) string {
			parts := re.FindStringSubmatch(m)
			if len(parts) != 3 {
				return m
			}
			prefix := parts[1]
			val := strings.TrimSpace(parts[2])
			// Keep quotes if present
			if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") && len(val) >= 2 {
				inner := val[1 : len(val)-1]
				return prefix + "\"" + mask(inner) + "\""
			}
			return prefix + mask(val)
		})
	}

	return yaml
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
		return ValidateWithDocker(cmd.Context(), cfg, docker)
	})
	if err != nil {
		return nil, err
	}

	// Create planner
	planner := CreatePlanner(docker, pr)

	return &CLIContext{
		Ctx:     cmd.Context(),
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
		plan, err = ctx.Planner.BuildPlan(ctx.Ctx, *ctx.Config)
		return err
	})

	return plan, err
}

// ApplyPlan executes the plan with progress tracking.
func (ctx *CLIContext) ApplyPlan() error {
	stdPr := ctx.Printer.(ui.StdPrinter)
	return ProgressOperation(stdPr, "Applying", func(pb *ui.Progress) error {
		return ctx.Planner.WithProgress(pb).Apply(ctx.Ctx, *ctx.Config)
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
