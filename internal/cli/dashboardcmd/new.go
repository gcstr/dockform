package dashboardcmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// keyMap defines the key bindings for the dashboard.
type keyMap struct {
	ToggleHelp key.Binding
	Quit       key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		ToggleHelp: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// ShortHelp returns the bindings shown in the expanded help bar.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.ToggleHelp, k.Quit}
}

// FullHelp returns all key bindings grouped.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.ToggleHelp, k.Quit},
	}
}

// layout constants
const (
	leftMinWidth   = 20
	leftMaxWidth   = 30
	rightMinWidth  = 20
	rightMaxWidth  = 30
	centerMinWidth = 40

	// Padding for container boxes (vertical, horizontal)
	// Used in Padding(paddingVertical, paddingHorizontal)
	paddingVertical   = 0
	paddingHorizontal = 1
	// Total horizontal padding applied to content width (left + right)
	totalHorizontalPadding = paddingHorizontal * 2

	// Overheads per column: paddings and margins used in renderColumns
	// box has Padding(0,1). Left/Center also have MarginRight(1).
	leftOverhead   = 3 // padding(2) + marginRight(1)
	centerOverhead = 3 // padding(2) + marginRight(1)
	rightOverhead  = 2 // padding(2)
)

// model is the Bubble Tea model for the dashboard.
type model struct {
	width  int
	height int

	keys keyMap
	help help.Model

	quitting bool
}

func newModel() model {
	return model{
		keys: newKeyMap(),
		help: help.New(),
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.ToggleHelp):
			m.help.ShowAll = !m.help.ShowAll
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}
	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	// Render three responsive columns and a help bar on the bottom
	helpBar := m.renderHelp()
	helpHeight := 0
	if helpBar != "" {
		helpHeight = lipgloss.Height(helpBar)
	}
	bodyHeight := m.height - helpHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	columns := m.renderColumns(bodyHeight)

	if helpBar == "" {
		return columns
	}
	return lipgloss.JoinVertical(lipgloss.Left, columns, helpBar)
}

// computeColumnWidths distributes available width among left, center, right.
func computeColumnWidths(total int) (left, center, right int) {
	if total <= 0 {
		return 1, 1, 1
	}

	// Subtract visual overhead (borders + margins) from available width to compute content widths
	usable := total - (leftOverhead + centerOverhead + rightOverhead)
	if usable < 3 {
		return 1, 1, 1
	}

	// Start with minimums for left and right; center gets remainder (may be < centerMinWidth on very small terminals)
	left = leftMinWidth
	right = rightMinWidth
	center = usable - left - right
	if center < 1 {
		center = 1
		// If even the strict minimums overflow, allow left/right to squeeze equally
		// to keep three columns visible.
		deficit := (left + right + center) - usable
		if deficit > 0 {
			// squeeze left then right as needed but keep at least width 1
			squeezeLeft := min(deficit/2+deficit%2, left-1)
			left -= squeezeLeft
			deficit -= squeezeLeft
			squeezeRight := min(deficit, right-1)
			right -= squeezeRight
		}
		return left, center, right
	}

	// If we have room beyond center minimum and left/right minimums, expand left/right up to max; center takes the rest
	minSum := leftMinWidth + rightMinWidth + centerMinWidth
	extra := usable - minSum
	if extra <= 0 {
		// Not enough space for center minimum; keep left/right at mins and let center be the remainder
		return left, max(1, center), right
	}

	// Expand left and right toward their maxima while keeping remaining for center
	expandLeft := min(extra/2, leftMaxWidth-leftMinWidth)
	expandRight := min(extra-expandLeft, rightMaxWidth-rightMinWidth)
	left = leftMinWidth + expandLeft
	right = rightMinWidth + expandRight
	center = usable - left - right
	if center < 1 {
		center = 1
	}
	return left, center, right
}

func (m model) renderColumns(bodyHeight int) string {
	// Styles for columns (no borders, just padding)
	box := lipgloss.NewStyle().Padding(paddingVertical, paddingHorizontal)
	// Height() sets the minimum content height
	innerHeight := bodyHeight
	if innerHeight < 0 {
		innerHeight = 0
	}
	leftStyle := box.Align(lipgloss.Left).MarginRight(1).Height(innerHeight)
	centerStyle := box.Align(lipgloss.Left).MarginRight(1).Height(innerHeight)
	// Right column intentionally does not set Height, so it only grows to its content
	rightStyle := box.Align(lipgloss.Left)

	// Titles (used for header titles)
	leftTitle := "Left"
	centerTitle := "Center"

	// Compute widths for this frame
	leftW, centerW, _ := computeColumnWidths(m.width)

	// Placeholder content with headers (headers sized to container content width)
	// Headers are plain text, pre-sized to exact width to prevent wrapping
	leftHeader := renderHeader(leftTitle, leftW)
	centerHeader := renderHeader(centerTitle, centerW)
	leftContent := leftHeader + "\n" + "placeholder"
	centerContent := centerHeader + "\n" + "placeholder"

	// Apply widths for left and center first
	leftView := leftStyle.Width(leftW).Render(leftContent)
	centerView := centerStyle.Width(centerW).Render(centerContent)

	// Compute remaining space for the right column and convert it to content width
	used := lipgloss.Width(lipgloss.JoinHorizontal(lipgloss.Top, leftView, centerView))
	remaining := m.width - used
	// Account for border(2) + padding(2), but add back 2 to fill the full width
	// (the overhead calculation seems to be too conservative by 2 cells)
	remainingContent := remaining - rightOverhead + 2
	if remainingContent < 1 {
		remainingContent = 1
	}
	// Right column: three stacked rows with headers sized to remainingContent
	r1Header := renderHeader("Row 1", remainingContent)
	r2Header := renderHeader("Row 2", remainingContent)
	r3Header := renderHeader("Row 3", remainingContent)
	rightRow1 := r1Header + "\n" + "placeholder"
	rightRow2 := r2Header + "\n" + "placeholder"
	rightRow3 := r3Header + "\n" + "placeholder"
	rightRows := lipgloss.JoinVertical(lipgloss.Left, rightRow1, rightRow2, rightRow3)
	rightView := rightStyle.Width(remainingContent).Render(rightRows)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftView, centerView, rightView)
}

func (m model) renderHelp() string {
	if m.width <= 0 {
		return m.help.View(m.keys)
	}
	return lipgloss.NewStyle().Width(m.width).Render(m.help.View(m.keys))
}

// renderHeader renders a single-line header like "◇ Title /////" that fills the full
// content width of the parent container, never wrapping. It clamps to the given width.
// The containerWidth should be the container's content width; the function accounts for
// horizontal padding internally.
func renderHeader(title string, containerWidth int) string {
	// Account for horizontal padding inside the container
	contentWidth := containerWidth - totalHorizontalPadding
	if contentWidth <= 0 {
		return ""
	}
	// Build the base: "◇ Title "
	base := fmt.Sprintf("◇ %s ", title)
	baseWidth := lipgloss.Width(base)

	// Calculate how many slashes we need to fill the exact width
	slashCount := contentWidth - baseWidth
	if slashCount < 0 {
		// If title is too long, truncate the whole thing
		return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(base)
	}

	// Build slashes
	slashes := strings.Repeat("/", slashCount)
	result := base + slashes

	// Force truncate at exact width to prevent any wrapping
	if lipgloss.Width(result) > contentWidth {
		// Truncate using lipgloss utilities
		return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(result)
	}

	return result
}

// New creates the `dockform dashboard` command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Launch the Dockform dashboard (fullscreen TUI)",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := tea.NewProgram(newModel(), tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	}
	return cmd
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
