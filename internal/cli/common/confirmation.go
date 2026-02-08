package common

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/gcstr/dockform/internal/ui"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

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

	tty := detectTTY(cmd)

	if tty.In && tty.Out {
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

	tty := detectTTY(cmd)

	if tty.In && tty.Out {
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
