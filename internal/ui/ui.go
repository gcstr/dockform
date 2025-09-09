package ui

import (
	"fmt"
	"regexp"
	"strings"

	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

var (
	styleInfo   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue
	styleNoop   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue
	styleAdd    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	styleRemove = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	styleChange = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow

	styleInfoPrefix  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true) // blue
	styleWarnPrefix  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // yellow
	styleErrorPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)  // red

	styleSectionTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#3478F6", Dark: "#4A9EFF"}).
				Padding(0, 0)

	styleUsingTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#000000", Dark: "#FFFFFF"}).
				Padding(0, 0)

	styleNestedSectionTitle = lipgloss.NewStyle().Bold(true).Italic(true)

	styleItalicName = lipgloss.NewStyle().Italic(true)
)

// Italic renders the given string in italic style (used for resource and file names).
func Italic(s string) string {
	return styleItalicName.Render(s)
}

// Section represents a header and its list of items for rendering.
type Section struct {
	Title string
	Items []DiffLine
}

// NestedSection represents a section that can contain nested subsections.
type NestedSection struct {
	Title    string
	Items    []DiffLine
	Sections []NestedSection
}

// RenderSectionedList renders sections with simple headers and two-space indented items.
func RenderSectionedList(sections []Section) string {
	var result strings.Builder
	firstSection := true

	for _, section := range sections {
		if len(section.Items) == 0 {
			continue
		}

		// Add blank line before section (except for the first one)
		if !firstSection {
			result.WriteString("\n")
		}
		firstSection = false

		// Section header with bold styling (override for "Using")
		titleStyle := styleSectionTitle
		if section.Title == "Using" {
			titleStyle = styleUsingTitle
		}
		result.WriteString(titleStyle.Render(section.Title))
		result.WriteString("\n")

		// Render items with two-space indentation and icons
		for _, item := range section.Items {
			result.WriteString("  ")
			// Info type items don't have icons, others do
			if item.Type != Info {
				result.WriteString(getIconForChangeType(item.Type))
				result.WriteString(" ")
			}
			result.WriteString(item.Message)
			result.WriteString("\n")
		}
	}

	return result.String()
}

// RenderNestedSections renders sections that can contain nested subsections.
func RenderNestedSections(sections []NestedSection) string {
	var result strings.Builder
	hasAnyContent := false

	// Check if we have any content to render
	for _, section := range sections {
		if len(section.Items) > 0 || len(section.Sections) > 0 {
			hasAnyContent = true
			break
		}
	}

	// If we have content, start with a blank line for proper spacing from previous output
	if hasAnyContent {
		result.WriteString("\n")
	}

	firstSection := true
	for _, section := range sections {
		hasContent := len(section.Items) > 0 || len(section.Sections) > 0
		if !hasContent {
			continue
		}

		// Add blank line before section (except for the first one)
		if !firstSection {
			result.WriteString("\n")
		}
		firstSection = false

		// Section header with styling (override for "Using")
		titleStyle := styleSectionTitle
		if section.Title == "Using" {
			titleStyle = styleUsingTitle
		}
		result.WriteString(titleStyle.Render(section.Title))
		result.WriteString("\n")

		// Render direct items with two-space indentation and icons
		for _, item := range section.Items {
			result.WriteString("  ")
			result.WriteString(getIconForChangeType(item.Type))
			result.WriteString(" ")
			result.WriteString(StripRedundantPrefixes(item.Message, section.Title))
			result.WriteString("\n")
		}

		// Render nested sections with additional indentation
		for _, nestedSection := range section.Sections {
			if len(nestedSection.Items) == 0 {
				continue
			}

			// Nested section header (if it has a title) - use plain bold style, not blue background
			if nestedSection.Title != "" {
				result.WriteString("  ")
				result.WriteString(styleNestedSectionTitle.Render(nestedSection.Title))
				result.WriteString("\n")
			}

			// Render nested items with four-space indentation
			for _, item := range nestedSection.Items {
				result.WriteString("    ")
				result.WriteString(getIconForChangeType(item.Type))
				result.WriteString(" ")
				result.WriteString(StripRedundantPrefixes(item.Message, section.Title))
				result.WriteString("\n")
			}
		}
	}

	return result.String()
}

// getIconForChangeType returns the appropriate icon for each change type.
func getIconForChangeType(changeType ChangeType) string {
	switch changeType {
	case Info:
		return styleInfo.Render("")
	case Noop:
		return styleNoop.Render("✓")
	case Add:
		return styleAdd.Render("↑")
	case Remove:
		return styleRemove.Render("×")
	case Change:
		return styleChange.Render("→")
	default:
		return ""
	}
}

// --- existing change-line utilities below ---

type ChangeType int

const (
	Info ChangeType = iota
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

// FormatPlanSummary renders a plan summary with bold "Plan:" prefix.
func FormatPlanSummary(createCount, changeCount, destroyCount int) string {
	boldPlan := lipgloss.NewStyle().Bold(true).Render("Plan:")
	summaryText := fmt.Sprintf(" %d to create, %d to change, and %d to destroy\n", createCount, changeCount, destroyCount)
	return boldPlan + summaryText
}

// StripRedundantPrefixes removes redundant "volume " and "network " prefixes from messages
// when they appear under their respective sections.
func StripRedundantPrefixes(message, sectionType string) string {
	switch sectionType {
	case "Volumes":
		if strings.HasPrefix(message, "volume ") {
			return message[7:] // Remove "volume " prefix
		}
	case "Networks":
		if strings.HasPrefix(message, "network ") {
			return message[8:] // Remove "network " prefix
		}
	}
	return message
}
