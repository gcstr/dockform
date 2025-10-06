package cli

import (
	"io"

	"github.com/gcstr/dockform/internal/cli/doctorcmd"
)

// removed wrapper; root now wires doctorcmd.New() directly

// Compatibility shims for tests referencing old symbols in package cli
type checkStatus = int

const (
	statusPass checkStatus = 0
	statusWarn checkStatus = 1
	statusFail checkStatus = 2
)

func printIndentedLines(w io.Writer, text string) { doctorcmd.PrintIndentedLines(w, text) }
