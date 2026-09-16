package ui

import (
	"fmt"
	"regexp"
	"strings"

	"io"
	"os"
	"strconv"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

var (
	// Define consistent colors using CompleteColor for precise control across all color profiles
	blue = lipgloss.CompleteColor{
		TrueColor: "#3B82F6", // Blue-500
		ANSI256:   "33",      // Bright blue in 256-color palette
		ANSI:      "4",       // Blue in 16-color palette
	}
	green = lipgloss.CompleteColor{
		TrueColor: "#22C55E", // Green-500
		ANSI256:   "46",      // Bright green in 256-color palette
		ANSI:      "2",       // Green in 16-color palette
	}
	red = lipgloss.CompleteColor{
		TrueColor: "#EF4444", // Red-500
		ANSI256:   "196",     // Bright red in 256-color palette
		ANSI:      "1",       // Red in 16-color palette
	}
	yellow = lipgloss.CompleteColor{
		TrueColor: "#EAB308", // Yellow-500
		ANSI256:   "220",     // Bright yellow in 256-color palette
		ANSI:      "3",       // Yellow in 16-color palette
	}

	styleInfo   = lipgloss.NewStyle().Foreground(blue)
	styleNoop   = lipgloss.NewStyle().Foreground(blue)
	styleAdd    = lipgloss.NewStyle().Foreground(green)
	styleRemove = lipgloss.NewStyle().Foreground(red)
	styleChange = lipgloss.NewStyle().Foreground(yellow)

	styleInfoPrefix  = lipgloss.NewStyle().Foreground(blue).Bold(true)
	styleWarnPrefix  = lipgloss.NewStyle().Foreground(yellow).Bold(true)
	styleErrorPrefix = lipgloss.NewStyle().Foreground(red).Bold(true)

	styleSectionTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#3478F6", Dark: "#4A9EFF"}).
				Padding(0, 0)

	styleUsingTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#000000", Dark: "#FFFFFF"}).
			Padding(0, 0)

	styleNestedSectionTitle = lipgloss.NewStyle().Bold(true).Italic(true)

	styleMuted = lipgloss.NewStyle().Faint(true)

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
	Footer   []DiffLine
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
// Blank-line separation applies only between THIS call's own top-level
// siblings; anything nested beneath a section (its .Sections, at any depth)
// renders tight against its own siblings — the original, unchanged two-level
// behavior. Use this for a plain (non-grouped) tree, e.g. a single
// ResourcePlan's Volumes/Networks/Stacks/Filesets/Containers sections.
func RenderNestedSections(sections []NestedSection) string {
	return renderSectionForest(sections, 1)
}

// RenderGroupedNestedSections is RenderNestedSections for a tree that has one
// extra wrapping level around it — e.g. a plan grouped by Docker context,
// where each context wraps the same Volumes/Networks/Stacks/Filesets/
// Containers tree one level deeper. Blank-line separation extends one level
// further in, so those wrapped resource-type sections keep the same visual
// separation from each other that they had before being wrapped; the
// per-stack/per-fileset leaf sections beneath them still render tight, as
// they always have.
func RenderGroupedNestedSections(sections []NestedSection) string {
	return renderSectionForest(sections, 2)
}

func renderSectionForest(sections []NestedSection, spacedLevels int) string {
	var result strings.Builder

	// If we have content, start with a blank line for proper spacing from previous output
	if sectionsHaveContent(sections) {
		result.WriteString("\n")
	}

	renderSections(&result, sections, 0, spacedLevels)

	return result.String()
}

// sectionsHaveContent reports whether any section in the list has an item,
// subsection, or footer line to render.
func sectionsHaveContent(sections []NestedSection) bool {
	for _, section := range sections {
		if sectionHasContent(section) {
			return true
		}
	}
	return false
}

func sectionHasContent(section NestedSection) bool {
	return len(section.Items) > 0 || len(section.Sections) > 0 || len(section.Footer) > 0
}

// maxSectionDepth bounds the recursion below: today's callers only ever
// produce at most three levels of section (group -> resource-type ->
// per-item); anything deeper is unexpected and is dropped rather than grown
// into indefinitely.
const maxSectionDepth = 2

// renderSections writes sections at the given depth (0 = top-level). Blank
// lines separate siblings for depth < spacedLevels; deeper levels render
// tight against each other. spacedLevels lets each entry point (see
// RenderNestedSections vs. RenderGroupedNestedSections above) state its own
// tree shape explicitly, rather than the renderer guessing it from the
// absolute depth alone — the same absolute depth means a different thing to
// each caller's tree.
func renderSections(result *strings.Builder, sections []NestedSection, depth, spacedLevels int) {
	if depth > maxSectionDepth {
		return
	}

	indent := strings.Repeat("  ", depth)
	itemIndent := strings.Repeat("  ", depth+1)

	firstSection := true
	for _, section := range sections {
		if !sectionHasContent(section) {
			continue
		}

		// Add a blank line before a sibling section at every level the
		// caller asked to have separated; deeper levels stay tight against
		// each other.
		if depth < spacedLevels {
			if !firstSection {
				result.WriteString("\n")
			}
			firstSection = false
		}

		// Section header with styling (override for "Using"). Depth 0 keeps
		// the original top-level style; any nested depth uses the existing
		// nested-section style.
		titleStyle := styleSectionTitle
		if depth > 0 {
			titleStyle = styleNestedSectionTitle
		}
		if section.Title == "Using" {
			titleStyle = styleUsingTitle
		}
		// Depth 0 always prints its header, even an empty one (unchanged from
		// before). Nested depths skip a blank title, exactly as the original
		// single level of nesting did.
		if depth == 0 || section.Title != "" {
			result.WriteString(indent)
			result.WriteString(titleStyle.Render(section.Title))
			result.WriteString("\n")
		}

		// Render direct items, indented one level deeper than this header.
		for _, item := range section.Items {
			result.WriteString(itemIndent)
			result.WriteString(getIconForChangeType(item.Type))
			result.WriteString(" ")
			result.WriteString(StripRedundantPrefixes(item.Message, section.Title))
			result.WriteString("\n")
		}

		// Recurse into nested sections one level deeper.
		renderSections(result, section.Sections, depth+1, spacedLevels)

		// Render footer lines at the same indent as this section's own items:
		// dim, no icon.
		for _, item := range section.Footer {
			result.WriteString(itemIndent)
			result.WriteString(styleMuted.Render(item.Message))
			result.WriteString("\n")
		}
	}
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
	// Suppress printing when rolling TUI is active
	if v := os.Getenv("DOCKFORM_TUI_ACTIVE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil && b {
			return
		}
	}
	if p.Out == nil {
		return
	}
	_, _ = fmt.Fprintf(p.Out, format+"\n", a...)
}

func (p StdPrinter) Info(format string, a ...any) {
	// Suppress printing when rolling TUI is active
	if v := os.Getenv("DOCKFORM_TUI_ACTIVE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil && b {
			return
		}
	}
	if p.Out == nil {
		return
	}
	// Avoid mixing with any active spinner on TTY
	clearCurrentLineIfTTY(p.Out)
	prefix := styleInfoPrefix.Render("[info]")
	_, _ = fmt.Fprintf(p.Out, "%s "+format+"\n", append([]any{prefix}, a...)...)
}

func (p StdPrinter) Warn(format string, a ...any) {
	// Suppress printing when rolling TUI is active
	if v := os.Getenv("DOCKFORM_TUI_ACTIVE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil && b {
			return
		}
	}
	if p.Err == nil {
		return
	}
	// Avoid mixing with any active spinner on TTY
	clearCurrentLineIfTTY(p.Err)
	prefix := styleWarnPrefix.Render("[warn]")
	_, _ = fmt.Fprintf(p.Err, "%s "+format+"\n", append([]any{prefix}, a...)...)
}

func (p StdPrinter) Error(format string, a ...any) {
	// Suppress printing when rolling TUI is active
	if v := os.Getenv("DOCKFORM_TUI_ACTIVE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil && b {
			return
		}
	}
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

// NestedSectionTitle renders a section heading that sits below a top-level
// header, the style RenderNestedSections applies at any depth past zero.
// Exported so internal/ui/applyview styles its group titles identically to the
// plan renderer rather than keeping its own look.
func NestedSectionTitle(title string) string {
	return styleNestedSectionTitle.Render(title)
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

// SuccessMark returns a green check mark for confirmations.
func SuccessMark() string {
	return styleAdd.Render("✓")
}

// RedText renders the provided text in red.
func RedText(s string) string {
	return styleRemove.Render(s)
}

// GreenText renders the provided text in green.
func GreenText(s string) string { return styleAdd.Render(s) }

// YellowText renders the provided text in yellow.
func YellowText(s string) string { return styleChange.Render(s) }

// BlueText renders the provided text in blue.
func BlueText(s string) string { return styleInfo.Render(s) }

// ConfirmToken renders a confirmation token (like "yes" or an identifier) in green, bold, italic.
func ConfirmToken(s string) string {
	return styleAdd.Bold(true).Italic(true).Render(s)
}
