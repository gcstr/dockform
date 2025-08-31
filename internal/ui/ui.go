package ui

import (
	"fmt"
	"regexp"

	"io"

	"github.com/charmbracelet/lipgloss"
)

var (
	styleNoop   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue
	styleAdd    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	styleRemove = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	styleChange = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow

	styleInfoPrefix  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true) // blue
	styleWarnPrefix  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // yellow
	styleErrorPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)  // red
)

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
	switch d.Type {
	case Noop:
		return styleNoop.Render("[no-op]") + " " + d.Message
	case Add:
		return styleAdd.Render("[add]") + "  " + d.Message
	case Remove:
		return styleRemove.Render("[remove]") + " " + d.Message
	case Change:
		return styleChange.Render("[change]") + " " + d.Message
	default:
		return d.Message
	}
}

func Line(t ChangeType, format string, a ...any) DiffLine {
	return DiffLine{Type: t, Message: fmt.Sprintf(format, a...)}
}

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes ANSI color codes for snapshot testing when needed.
func StripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

// Printer centralizes user-facing output. It routes informational messages to
// stdout and warnings/errors to stderr, ready for future styling via lipgloss.
type Printer interface {
	Info(format string, a ...any)
	Warn(format string, a ...any)
	Error(format string, a ...any)
}

// StdPrinter writes Info to Out and Warn/Error to Err.
type StdPrinter struct {
	Out io.Writer
	Err io.Writer
}

func (p StdPrinter) Info(format string, a ...any) {
	if p.Out == nil {
		return
	}
	prefix := styleInfoPrefix.Render("[info]")
	_, _ = fmt.Fprintf(p.Out, "%s "+format+"\n", append([]any{prefix}, a...)...)
}

func (p StdPrinter) Warn(format string, a ...any) {
	if p.Err == nil {
		return
	}
	prefix := styleWarnPrefix.Render("[warn]")
	_, _ = fmt.Fprintf(p.Err, "%s "+format+"\n", append([]any{prefix}, a...)...)
}

func (p StdPrinter) Error(format string, a ...any) {
	if p.Err == nil {
		return
	}
	prefix := styleErrorPrefix.Render("[error]")
	_, _ = fmt.Fprintf(p.Err, "%s "+format+"\n", append([]any{prefix}, a...)...)
}

// NoopPrinter discards all output; useful as a default or in tests.
type NoopPrinter struct{}

func (NoopPrinter) Info(string, ...any)  {}
func (NoopPrinter) Warn(string, ...any)  {}
func (NoopPrinter) Error(string, ...any) {}
