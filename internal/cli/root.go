package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/spf13/cobra"
)

// verbose controls extra error detail printing.
var verbose bool

// build-time variables injected via -ldflags; defaults are used for dev builds.
var (
	version   = "0.1.0-dev"
	commit    = ""
	date      = ""
	builtBy   = ""
	goVersion = ""
)

// Execute runs the root command and handles error formatting and exit codes.
func Execute() int {
	cmd := newRootCmd()
	if err := cmd.ExecuteContext(context.Background()); err != nil {
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
	return 0
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "dockform",
		Short:         "Manage Docker Compose projects declaratively",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringP("config", "c", "", "Path to configuration file or directory (defaults: dockform.yml, dockform.yaml, Dockform.yml, Dockform.yaml in current directory)")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose error output")

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newPlanCmd())
	cmd.AddCommand(newApplyCmd())
	cmd.AddCommand(newDestroyCmd())
	cmd.AddCommand(newValidateCmd())
	cmd.AddCommand(newSecretCmd())
	cmd.AddCommand(newManifestCmd())
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newVolumeCmd())

	// Register optional developer-only commands
	registerDocsCmd(cmd)

	cmd.SetHelpTemplate(cmd.HelpTemplate() + "\n\nProject home: https://github.com/gcstr/dockform\n")

	cmd.SetVersionTemplate(fmt.Sprintf("%s\n", VersionSimple()))
	cmd.Version = VersionSimple()

	return cmd
}

func Version() string {
	return version
}

// VersionSimple returns version number with build info for --version flag
func VersionSimple() string {
	v := version
	if commit != "" {
		v += " (" + commit[:7] + ")" // Short commit hash only
	}
	return v
}

// VersionDetailed returns version info with build metadata if available.
func VersionDetailed() string {
	v := version
	if commit != "" {
		v += " (" + commit
		if date != "" {
			v += ", " + date
		}
		if builtBy != "" {
			v += ", " + builtBy
		}
		v += ")"
	}
	return v
}

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
