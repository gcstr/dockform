package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/cli/applycmd"
	"github.com/gcstr/dockform/internal/cli/buildinfo"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/cli/composecmd"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd"
	"github.com/gcstr/dockform/internal/cli/destroycmd"
	"github.com/gcstr/dockform/internal/cli/doctorcmd"
	"github.com/gcstr/dockform/internal/cli/imagescmd"
	"github.com/gcstr/dockform/internal/cli/initcmd"
	"github.com/gcstr/dockform/internal/cli/manifestcmd"
	"github.com/gcstr/dockform/internal/cli/plancmd"
	"github.com/gcstr/dockform/internal/cli/secretcmd"
	"github.com/gcstr/dockform/internal/cli/validatecmd"
	"github.com/gcstr/dockform/internal/cli/versioncmd"
	"github.com/gcstr/dockform/internal/cli/volumecmd"
	"github.com/gcstr/dockform/internal/logger"
	"github.com/spf13/cobra"
)

// verbose controls extra error detail printing.
var verbose bool

// build-time variables injected via -ldflags are now in buildinfo.
type logCloserKey struct{}

// Execute runs the root command and handles error formatting and exit codes.
// It accepts a context that should be cancelled on interrupt signals.
func Execute(ctx context.Context) int {
	cmd := newRootCmd()
	err := cmd.ExecuteContext(ctx)
	closeLogCloser(cmd)
	common.TeardownSSHMux(cmd)
	if err != nil {
		// Check if the error is a context cancellation (user interrupted)
		// If so, don't print the error and exit with code 130 (128 + SIGINT)
		if errors.Is(err, context.Canceled) {
			return 130
		}
		printUserFriendly(err)
		switch {
		case apperr.IsKind(err, apperr.InvalidInput):
			return 2
		case apperr.IsKind(err, apperr.Unavailable) || apperr.IsKind(err, apperr.Timeout):
			return 69
		case apperr.IsKind(err, apperr.External):
			return 70
		default:
			return 1
		}
	}
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return 130
	}
	return 0
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "dockform",
		Short:         "Manage Docker Compose projects declaratively",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Initialize structured logger based on flags/environment
			level, _ := cmd.Flags().GetString("log-level")
			format, _ := cmd.Flags().GetString("log-format")
			logFile, _ := cmd.Flags().GetString("log-file")
			noColor, _ := cmd.Flags().GetBool("no-color")

			// Default: do not emit structured logs to the terminal.
			// When verbose is true, send logs to stderr using the configured format (auto→pretty on TTY).
			primaryOut := io.Discard
			if verbose {
				primaryOut = cmd.ErrOrStderr()
			}
			l, closer, err := logger.New(logger.Options{Out: primaryOut, Level: level, Format: format, NoColor: noColor, LogFile: logFile})
			if err != nil {
				return err
			}
			if closer != nil {
				root := cmd.Root()
				root.SetContext(context.WithValue(root.Context(), logCloserKey{}, closer))
			}
			// Attach per-run fields
			runID := logger.NewRunID()
			l = l.With("run_id", runID)
			// Best effort to derive command path
			commandPath := cmd.CommandPath()
			l = l.With("command", commandPath)
			cmd.SetContext(logger.WithContext(cmd.Context(), l))
			return nil
		},
	}

	cmd.PersistentFlags().String("manifest", "", "Path to manifest file or directory (defaults: dockform.yml, dockform.yaml, Dockform.yml, Dockform.yaml in current directory)")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose error output")
	// Logging flags
	cmd.PersistentFlags().String("log-level", "info", "Log level: debug, info, warn, error")
	cmd.PersistentFlags().String("log-format", "auto", "Log format: auto, pretty, json")
	cmd.PersistentFlags().String("log-file", "", "Write logs to file using the format specified by --log-format (in addition to stderr)")
	cmd.PersistentFlags().Bool("no-color", false, "Disable color in pretty logs")
	cmd.PersistentFlags().Bool("ssh-multiplex", true, "Reuse one SSH connection per host for a run (ControlMaster); disable with --ssh-multiplex=false or DOCKFORM_SSH_MULTIPLEX=false")

	cmd.AddCommand(initcmd.New())
	cmd.AddCommand(plancmd.New())
	cmd.AddCommand(applycmd.New())
	cmd.AddCommand(destroycmd.New())
	cmd.AddCommand(validatecmd.New())
	cmd.AddCommand(secretcmd.New())
	cmd.AddCommand(manifestcmd.New())
	// New top-level compose command
	cmd.AddCommand(composecmd.New())
	cmd.AddCommand(versioncmd.New())
	cmd.AddCommand(volumecmd.New())
	cmd.AddCommand(doctorcmd.New())
	cmd.AddCommand(dashboardcmd.New())
	cmd.AddCommand(imagescmd.New())

	// Register optional developer-only commands
	registerDocsCmd(cmd)

	cmd.SetHelpTemplate(cmd.HelpTemplate() + "\n\nProject home: https://github.com/gcstr/dockform\n")

	cmd.SetVersionTemplate(fmt.Sprintf("%s\n", buildinfo.VersionSimple()))
	cmd.Version = buildinfo.VersionSimple()

	return cmd
}

// Version helpers are provided by buildinfo now.

// TestPrintUserFriendly exposes printUserFriendly for testing
func TestPrintUserFriendly(err error) {
	printUserFriendly(err)
}

// TestNewRootCmd exposes the root command for integration tests.
func TestNewRootCmd() *cobra.Command { return newRootCmd() }

func closeLogCloser(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	root := cmd.Root()
	if root.Context() == nil {
		return
	}
	v := root.Context().Value(logCloserKey{})
	if v == nil {
		return
	}
	if closer, ok := v.(io.Closer); ok && closer != nil {
		_ = closer.Close()
	}
}

func provideExternalErrorHints(err error) {
	msg := err.Error()
	// err.Error() on an *apperr.E collapses to Op+Msg and drops the wrapped
	// cause, so the captured command stderr (which lives on the innermost
	// error's Msg) would never be visible here. Match the known failure
	// patterns against the deepest message in the chain instead.
	deepest := apperr.DeepestMessage(err)

	if strings.Contains(msg, "invalid compose file") || strings.Contains(deepest, "invalid compose file") {
		fmt.Fprintln(os.Stderr, "\nHint: Check your Docker Compose file syntax")
		fmt.Fprintln(os.Stderr, "      Try: docker compose config --quiet")
		fmt.Fprintln(os.Stderr, "      Try: docker compose -f <file> config")
		return
	}

	if hint := composeStderrHint(deepest); hint != "" {
		fmt.Fprintln(os.Stderr, "\nHint:", hint)
		return
	}

	if strings.Contains(msg, "compose") {
		fmt.Fprintln(os.Stderr, "\nHint: Docker Compose operation failed")
		fmt.Fprintln(os.Stderr, "      Check your compose files and Docker daemon status")
		return
	}
}

// imageRefPattern extracts an image[:tag] reference from a "manifest for
// <image:tag> not found" style message emitted by docker/compose pulls.
var imageRefPattern = regexp.MustCompile(`(?i)manifest for (\S+) not found`)

// composeStderrHint inspects captured compose/docker stderr for known failure
// signatures and returns an actionable, single-line hint. It returns "" when
// no known pattern matches, so callers can fall back to a generic hint.
//
// The auth case is checked before the image case on purpose: messages like
// "pull access denied for foo/bar, repository does not exist or may require
// authorization" contain both signatures and are auth problems first.
func composeStderrHint(msg string) string {
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "denied") || strings.Contains(lower, "unauthorized"):
		return "Registry authentication problem. Check your credentials (docker login) and image access permissions."
	case strings.Contains(lower, "manifest unknown") ||
		strings.Contains(lower, "repository does not exist") ||
		imageRefPattern.MatchString(msg):
		// Only genuinely image-pull-shaped messages reach here; a bare
		// "not found" (e.g. "network foo not found") must NOT match, since a
		// wrong specific hint is worse than the generic fallback.
		if m := imageRefPattern.FindStringSubmatch(msg); len(m) == 2 {
			return fmt.Sprintf("Image %q does not exist in the registry (or the tag is wrong). Check the image name and tag.", m[1])
		}
		return "The referenced image or tag does not exist in the registry. Check the image name and tag."
	case strings.Contains(lower, "no space left"):
		return "The Docker host is out of disk space. Free up space on the daemon host and try again."
	default:
		return ""
	}
}

func provideDockerTroubleshootingHints(err error) {
	msg := err.Error()

	fmt.Fprintln(os.Stderr, "\nHint: Is the Docker daemon running and reachable from the selected context?")

	// Context-specific hints
	if strings.Contains(msg, "context=") && !strings.Contains(msg, "context=default") {
		fmt.Fprintln(os.Stderr, "      Try: docker context ls")
		fmt.Fprintln(os.Stderr, "      Try: docker --context <name> ps")
	} else {
		fmt.Fprintln(os.Stderr, "      Try: docker ps")
	}

	// OS-specific hints
	if strings.Contains(msg, "unix:///var/run/docker.sock") {
		fmt.Fprintln(os.Stderr, "      On macOS/Linux: Check if Docker Desktop is running")
		fmt.Fprintln(os.Stderr, "      On Linux: Try 'sudo systemctl start docker'")
	} else if strings.Contains(msg, "npipe") || strings.Contains(msg, "windows") {
		fmt.Fprintln(os.Stderr, "      On Windows: Check if Docker Desktop is running")
	}
}

func printUserFriendly(err error) {
	var e *apperr.E
	if errors.As(err, &e) {
		// For External errors (like Docker failures), always show the underlying error
		// even in non-verbose mode to help users understand what went wrong
		if apperr.IsKind(err, apperr.External) {
			// Show both the context message and the underlying error
			if e.Msg != "" {
				fmt.Fprintf(os.Stderr, "Error: %s\n", e.Msg)
			}
			// A MultiError means several contexts/stacks failed independently.
			// Surface each child's deepest cause (e.g. captured compose stderr)
			// and an actionable hint for that specific failure, instead of
			// collapsing everything into one generic message.
			var multi *apperr.MultiError
			if errors.As(e.Err, &multi) {
				printMultiErrorDetail(multi)
			} else if e.Err != nil {
				// Otherwise show the deepest message in the chain (e.g. the
				// captured command stderr), not just the immediate child's Msg.
				fmt.Fprintf(os.Stderr, "%s\n", apperr.DeepestMessage(e.Err))
			}
		} else {
			// Non-External errors: use existing logic
			if e.Msg != "" {
				fmt.Fprintf(os.Stderr, "Error: %s\n", e.Msg)
			} else {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
			}
		}
		// Verbose mode prints chain details
		if verbose {
			fmt.Fprintln(os.Stderr, "Detail:", err)
		}
		// Contextual hints
		if apperr.IsKind(err, apperr.Unavailable) {
			provideDockerTroubleshootingHints(err)
		} else if apperr.IsKind(err, apperr.External) {
			var multi *apperr.MultiError
			if !errors.As(e.Err, &multi) {
				provideExternalErrorHints(err)
			}
		}
		return
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
}

// printMultiErrorDetail prints, for each child error in a MultiError, its
// deepest underlying message (e.g. captured compose/docker stderr) along with
// a per-failure actionable hint when one of the known patterns matches.
func printMultiErrorDetail(multi *apperr.MultiError) {
	for _, child := range multi.Errors {
		detail := apperr.DeepestMessage(child)
		var ctxErr *apperr.ContextError
		if errors.As(child, &ctxErr) {
			fmt.Fprintf(os.Stderr, "context %s: %s\n", ctxErr.ContextName, detail)
		} else {
			fmt.Fprintf(os.Stderr, "%s\n", detail)
		}
		if hint := composeStderrHint(detail); hint != "" {
			fmt.Fprintln(os.Stderr, "  Hint:", hint)
		} else {
			fmt.Fprintln(os.Stderr, "  Hint: Docker Compose operation failed")
			fmt.Fprintln(os.Stderr, "        Check your compose files and Docker daemon status")
		}
	}
}
