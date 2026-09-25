package imagescmd

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gcstr/dockform/internal/ui"
)

// legendWidth is the column hints wrap at, so the legend stays readable in a
// narrow terminal.
const legendWidth = 80

// Short labels for table cells. The legend under the table explains each one
// that appears, so rows stay narrow.
const (
	labelError       = "error"
	labelDigest      = "digest changed"
	labelChanged     = "changed" // images check's DIGEST column
	labelNotApplied  = "not applied"
	labelTagNotFound = "tag not found"
)

// legendHint explains a label in one short line, plus the command that fixes
// it (if any) on a line of its own, so a command is never split by wrapping.
type legendHint struct {
	text string
	fix  string
}

var legendHints = map[string]legendHint{
	labelDigest:      {"The container runs an older image than its tag now points to.", "dockform images pull --recreate"},
	labelChanged:     {"The container runs an older image than its tag now points to.", "dockform images pull --recreate"},
	labelNotApplied:  {"The compose file changed since the last apply.", "dockform apply"},
	labelTagNotFound: {"Newer tag found, but the current tag isn't in the compose files.", ""},
}

// legend collects the labels a table used, in first-use order, plus one line
// per error, and prints them once below the tables.
type legend struct {
	labels []string
	errors []string
}

func (l *legend) use(label string) {
	for _, have := range l.labels {
		if have == label {
			return
		}
	}
	l.labels = append(l.labels, label)
}

func (l *legend) addError(stack, image, msg string) {
	l.use(labelError)
	l.errors = append(l.errors, stack+" "+image+": "+msg)
}

func (l *legend) render(pr ui.Printer) {
	if len(l.labels) == 0 {
		return
	}
	width := 0
	for _, label := range l.labels {
		if len(label) > width {
			width = len(label)
		}
	}
	dim := lipgloss.NewStyle().Faint(true)
	indent := strings.Repeat(" ", 2+width+2)

	pr.Plain("")
	for _, label := range l.labels {
		var lines []string
		if label == labelError {
			for _, e := range l.errors {
				lines = append(lines, wrapWords(e, legendWidth-len(indent))...)
			}
		} else {
			hint := legendHints[label]
			lines = wrapWords(hint.text, legendWidth-len(indent))
			if hint.fix != "" {
				lines = append(lines, "Run "+hint.fix)
			}
		}
		for i, line := range lines {
			if i == 0 {
				pr.Plain("  %s  %s", ui.YellowText(padRight(label, width)), dim.Render(line))
				continue
			}
			pr.Plain("%s%s", indent, dim.Render(line))
		}
	}
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// wrapWords breaks text into lines of at most width characters, splitting on
// spaces. A word longer than width gets a line of its own.
func wrapWords(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
