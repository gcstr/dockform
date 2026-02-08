package common

import (
	"context"
	"os"

	"github.com/gcstr/dockform/internal/ui"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// SpinnerOperation runs an operation with a spinner, automatically handling start/stop.
func SpinnerOperation(pr ui.StdPrinter, message string, operation func() error) error {
	spinner := ui.NewSpinner(pr.Out, message)
	spinner.Start()
	err := operation()
	spinner.Stop()
	return err
}

// DynamicSpinnerOperation runs an operation with a spinner that can be updated.
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
