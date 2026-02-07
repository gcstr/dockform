package common

import (
	"os"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// ttyStatus reports whether the command's stdin and stdout are terminals.
type ttyStatus struct {
	In  bool
	Out bool
}

// detectTTY checks whether cmd's stdin and stdout are connected to a terminal.
func detectTTY(cmd *cobra.Command) ttyStatus {
	var s ttyStatus
	if f, ok := cmd.InOrStdin().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		s.In = true
	}
	if f, ok := cmd.OutOrStdout().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		s.Out = true
	}
	return s
}
