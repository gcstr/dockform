package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/spf13/cobra"
)

// verbose controls extra error detail printing.
var verbose bool

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

	cmd.PersistentFlags().StringP("config", "c", "", "Path to configuration file or directory (defaults to dockform.yml or dockform.yaml in current directory)")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose error output")

	cmd.AddCommand(newPlanCmd())
	cmd.AddCommand(newApplyCmd())
	cmd.AddCommand(newFilesetCmd())
	cmd.AddCommand(newValidateCmd())
	cmd.AddCommand(newSecretCmd())
	cmd.AddCommand(newManifestCmd())

	cmd.SetHelpTemplate(cmd.HelpTemplate() + "\n\nProject home: https://github.com/gcstr/dockform\n")

	cmd.SetVersionTemplate(fmt.Sprintf("%s\n", Version()))
	cmd.Version = Version()

	return cmd
}

func Version() string { return "0.1.0-dev" }

func printUserFriendly(err error) {
	var e *apperr.E
	if errors.As(err, &e) {
		// Short human message
		if e.Msg != "" {
			fmt.Fprintf(os.Stderr, "Error: %s\n", e.Msg)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
		}
		// Verbose mode prints chain details
		if verbose {
			fmt.Fprintln(os.Stderr, "Detail:", err)
		}
		// Contextual hints
		if apperr.IsKind(err, apperr.Unavailable) {
			fmt.Fprintln(os.Stderr, "Hint: Is the Docker daemon running and reachable from the selected context?")
		}
		return
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
}
