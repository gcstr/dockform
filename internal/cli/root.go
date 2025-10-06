package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/cli/applycmd"
	"github.com/gcstr/dockform/internal/cli/buildinfo"
	"github.com/gcstr/dockform/internal/cli/composecmd"
	"github.com/gcstr/dockform/internal/cli/destroycmd"
	"github.com/gcstr/dockform/internal/cli/doctorcmd"
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

// Execute runs the root command and handles error formatting and exit codes.
// It accepts a context that should be cancelled on interrupt signals.
func Execute(ctx context.Context) int {
	cmd := newRootCmd()
	if err := cmd.ExecuteContext(ctx); err != nil {
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
				// Ensure file is closed on process exit; cobra doesn't give a post-run hook for root easily.
				// We register a finalizer via command context.
				cmd.SetContext(context.WithValue(cmd.Context(), struct{ k string }{"logCloser"}, closer))
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

	cmd.PersistentFlags().StringP("config", "c", "", "Path to configuration file or directory (defaults: dockform.yml, dockform.yaml, Dockform.yml, Dockform.yaml in current directory)")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose error output")
	// Logging flags
	cmd.PersistentFlags().String("log-level", "info", "Log level: debug, info, warn, error")
	cmd.PersistentFlags().String("log-format", "auto", "Log format: auto, pretty, json")
	cmd.PersistentFlags().String("log-file", "", "Write JSON logs to file (in addition to stderr)")
	cmd.PersistentFlags().Bool("no-color", false, "Disable color in pretty logs")

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

func provideExternalErrorHints(err error) {
	msg := err.Error()

	if strings.Contains(msg, "invalid compose file") {
		fmt.Fprintln(os.Stderr, "\nHint: Check your Docker Compose file syntax")
		fmt.Fprintln(os.Stderr, "      Try: docker compose config --quiet")
		fmt.Fprintln(os.Stderr, "      Try: docker compose -f <file> config")
		return
	}

	if strings.Contains(msg, "compose") {
		fmt.Fprintln(os.Stderr, "\nHint: Docker Compose operation failed")
		fmt.Fprintln(os.Stderr, "      Check your compose files and Docker daemon status")
		return
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
			// Always show the underlying error for External errors
			if e.Err != nil {
				// Check if the underlying error has useful information
				var underlyingAppErr *apperr.E
				if errors.As(e.Err, &underlyingAppErr) && underlyingAppErr.Msg != "" {
					// If the underlying error is also an apperr.E with a message, show it
					fmt.Fprintf(os.Stderr, "%s\n", underlyingAppErr.Msg)
				} else {
					// Otherwise show the full error string
					fmt.Fprintf(os.Stderr, "%s\n", e.Err.Error())
				}
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
			provideExternalErrorHints(err)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
}
