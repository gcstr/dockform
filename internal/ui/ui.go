package ui

import (
	"fmt"
	"regexp"

	"github.com/charmbracelet/lipgloss"
)

var (
	styleNoop   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue
	styleAdd    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	styleRemove = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	styleChange = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
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
