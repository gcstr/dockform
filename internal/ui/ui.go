package ui

import (
	"fmt"
	"regexp"

	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	lglist "github.com/charmbracelet/lipgloss/list"
	"github.com/mattn/go-isatty"
)

var (
	styleNoop   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue
	styleAdd    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	styleRemove = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	styleChange = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow

	styleInfoPrefix  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true) // blue
	styleWarnPrefix  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // yellow
	styleErrorPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)  // red

	styleSectionTitle = lipgloss.NewStyle().Bold(true)
)

// Section represents a header and its list of items for rendering.
type Section struct {
	Title string
	Items []DiffLine
}

// ListStyles centralizes lipgloss list styles for consistent UX.
var ListStyles = struct {
	OuterEnumStyle lipgloss.Style
	OuterItemStyle lipgloss.Style
	InnerEnumStyle lipgloss.Style
	InnerItemStyle lipgloss.Style
}{
	OuterEnumStyle: lipgloss.NewStyle(),
	OuterItemStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69")).MarginTop(1),
	InnerEnumStyle: lipgloss.NewStyle().MarginRight(1).MarginLeft(1), // margin only; color per-item
	InnerItemStyle: lipgloss.NewStyle(),
}

// RenderSectionedList renders sections as a nested lipgloss list with configured styles.
func RenderSectionedList(sections []Section) string {
	// Build alternating args: Title, innerList, Title, innerList, ...
	args := make([]any, 0, len(sections)*2)
	for _, s := range sections {
		if len(s.Items) == 0 {
			continue
		}
		vals := make([]any, len(s.Items))
		for i, it := range s.Items {
			vals[i] = it.String()
		}
		// Per-item colored enumerator based on s.Items index
		itemsRef := s.Items
		enum := func(_ lglist.Items, i int) string {
			if i < 0 || i >= len(itemsRef) {
				return ""
			}
			switch itemsRef[i].Type {
			case Noop:
				return styleNoop.Render("●")
			case Add:
				return styleAdd.Render("↑")
			case Remove:
				return styleRemove.Render("↓")
			case Change:
				return styleChange.Render("→")
			default:
				return ""
			}
		}
		inner := lglist.New(vals...).
			Enumerator(enum).
			EnumeratorStyle(ListStyles.InnerEnumStyle).
			ItemStyle(ListStyles.InnerItemStyle)
		args = append(args, s.Title, inner)
	}
	if len(args) == 0 {
		return ""
	}
	// Empty enumerator for section headers
	emptyEnum := func(_ lglist.Items, _ int) string { return "" }
	outer := lglist.New(args...).
		Enumerator(emptyEnum).
		EnumeratorStyle(ListStyles.OuterEnumStyle).
		ItemStyle(ListStyles.OuterItemStyle)
	return outer.String() + "\n"
}

// --- existing change-line utilities below ---

type ChangeType int

const (
	Noop ChangeType = iota
	Add
	Remove
	Change
)

type DiffLine struct {
	Type    ChangeType
	Message string
}

func (d DiffLine) String() string {
	return d.Message
}

func Line(t ChangeType, format string, a ...any) DiffLine {
	return DiffLine{Type: t, Message: fmt.Sprintf(format, a...)}
}

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes ANSI color codes for snapshot testing when needed.
func StripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

// clearCurrentLineIfTTY clears the current terminal line when writing to a TTY.
func clearCurrentLineIfTTY(w io.Writer) {
	if f, ok := w.(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		// Carriage return and clear line escape
		_, _ = fmt.Fprint(w, "\r\x1b[2K")
	}
}

// Printer centralizes user-facing output. It routes informational messages to
// stdout and warnings/errors to stderr, ready for future styling via lipgloss.
type Printer interface {
	// Plain writes to stdout without any prefix or styling.
	Plain(format string, a ...any)
	// Info writes to stdout with an [info] prefix.
	Info(format string, a ...any)
	// Warn writes to stderr with a [warn] prefix.
	Warn(format string, a ...any)
	// Error writes to stderr with an [error] prefix.
	Error(format string, a ...any)
}

// StdPrinter writes Info to Out and Warn/Error to Err.
type StdPrinter struct {
	Out io.Writer
	Err io.Writer
}

func (p StdPrinter) Plain(format string, a ...any) {
	if p.Out == nil {
		return
	}
	_, _ = fmt.Fprintf(p.Out, format+"\n", a...)
}

func (p StdPrinter) Info(format string, a ...any) {
	if p.Out == nil {
		return
	}
	// Avoid mixing with any active spinner on TTY
	clearCurrentLineIfTTY(p.Out)
	prefix := styleInfoPrefix.Render("[info]")
	_, _ = fmt.Fprintf(p.Out, "%s "+format+"\n", append([]any{prefix}, a...)...)
}

func (p StdPrinter) Warn(format string, a ...any) {
	if p.Err == nil {
		return
	}
	// Avoid mixing with any active spinner on TTY
	clearCurrentLineIfTTY(p.Err)
	prefix := styleWarnPrefix.Render("[warn]")
	_, _ = fmt.Fprintf(p.Err, "%s "+format+"\n", append([]any{prefix}, a...)...)
}

func (p StdPrinter) Error(format string, a ...any) {
	if p.Err == nil {
		return
	}
	// Avoid mixing with any active spinner on TTY
	clearCurrentLineIfTTY(p.Err)
	prefix := styleErrorPrefix.Render("[error]")
	_, _ = fmt.Fprintf(p.Err, "%s "+format+"\n", append([]any{prefix}, a...)...)
}

// NoopPrinter discards all output; useful as a default or in tests.
type NoopPrinter struct{}

func (NoopPrinter) Plain(string, ...any) {}
func (NoopPrinter) Info(string, ...any)  {}
func (NoopPrinter) Warn(string, ...any)  {}
func (NoopPrinter) Error(string, ...any) {}

// SectionTitle renders a bold section header for grouped output.
func SectionTitle(title string) string {
	return styleSectionTitle.Render(title)
}
