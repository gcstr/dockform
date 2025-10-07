package dashboardcmd

import (
	"os"

	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/components"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/theme"
	"github.com/spf13/cobra"
)

// appStyle fills the entire alt screen with a uniform background

// keyMap defines the key bindings for the dashboard.
type keyMap struct {
	ToggleHelp key.Binding
	Quit       key.Binding
	Filter     key.Binding
	MoveUp     key.Binding
	MoveDown   key.Binding
	NextPage   key.Binding
	PrevPage   key.Binding
	CyclePane  key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		ToggleHelp: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter stacks"),
		),
		MoveUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		MoveDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		NextPage: key.NewBinding(
			key.WithKeys("right", "l", "pgdn"),
			key.WithHelp("→/l/pgdn", "next page"),
		),
		PrevPage: key.NewBinding(
			key.WithKeys("left", "h", "pgup"),
			key.WithHelp("←/h/pgup", "prev page"),
		),
		CyclePane: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next pane"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// ShortHelp returns the bindings shown in the expanded help bar.
func (k keyMap) ShortHelp() []key.Binding {
	// Only show minimal help; full help is toggled with '?'
	return []key.Binding{k.ToggleHelp, k.Quit}
}

// FullHelp returns all key bindings grouped.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.MoveUp, k.MoveDown, k.NextPage, k.PrevPage}, // navigation column
		{k.Filter, k.Quit}, // actions column
	}
}

// layout constants
const (
	leftMinWidth   = 30
	leftMaxWidth   = 40
	rightMinWidth  = 20
	rightMaxWidth  = 30
	centerMinWidth = 40

	// Padding for container boxes (vertical, horizontal)
	// Used in Padding(paddingVertical, paddingHorizontal)
	paddingVertical   = 0
	paddingHorizontal = 1
	// Total horizontal padding applied to content width (left + right)
	totalHorizontalPadding = paddingHorizontal * 2

	// Overheads per column: paddings used in renderColumns
	// box has Padding(0,1). We now avoid using margins to prevent unstyled gaps.
	leftOverhead   = 2 // padding(2)
	centerOverhead = 4 // padding(2) + extra center horizontal padding(2)
	rightOverhead  = 2 // padding(2)
)

// model is the Bubble Tea model for the dashboard.
type model struct {
	width  int
	height int

	keys      keyMap
	help      help.Model
	list      list.Model
	logsPager components.LogsPager

	quitting   bool
	activePane int
}

func newModel() model {
	// Create sample project items
	items := []list.Item{
		components.StackItem{TitleText: "Vaultwarden", Containers: []string{"vaultwarden", "vaultwarden/server:1.32.1"}, Status: "● Running - 8 hours"},
		components.StackItem{TitleText: "PostgreSQL", Containers: []string{"postgres", "postgres:16-alpine"}, Status: "● Running - 2 days"},
		components.StackItem{TitleText: "Redis", Containers: []string{"redis", "redis:7-alpine"}, Status: "● Running - 1 week"},
		components.StackItem{TitleText: "Nginx", Containers: []string{"nginx", "nginx:latest"}, Status: "○ Stopped"},
		components.StackItem{TitleText: "Traefik", Containers: []string{"traefik", "traefik:v3.0"}, Status: "● Running - 3 hours"},
		components.StackItem{TitleText: "Vaultwarden", Containers: []string{"vaultwarden", "vaultwarden/server:1.32.1"}, Status: "● Running - 8 hours"},
		components.StackItem{TitleText: "PostgreSQL", Containers: []string{"postgres", "postgres:16-alpine"}, Status: "● Running - 2 days"},
		components.StackItem{TitleText: "Redis", Containers: []string{"redis", "redis:7-alpine"}, Status: "● Running - 1 week"},
		components.StackItem{TitleText: "Nginx", Containers: []string{"nginx", "nginx:latest"}, Status: "○ Stopped"},
		components.StackItem{TitleText: "Traefik", Containers: []string{"traefik", "traefik:v3.0"}, Status: "● Running - 3 hours"},
	}

	// Create the list with custom delegate
	delegate := components.StacksDelegate{}
	projectList := list.New(items, delegate, 0, 0)
	projectList.SetShowTitle(false)
	projectList.SetShowStatusBar(false)
	projectList.SetFilteringEnabled(true)
	projectList.SetShowHelp(false)
	projectList.SetShowPagination(true)

	projectList.FilterInput.Prompt = "> "
	projectList.FilterInput.Placeholder = "Stack, container, or image..."
	projectList.Styles.Filter.Focused.Prompt = lipgloss.NewStyle().Foreground(theme.Success)
	projectList.Styles.Filter.Blurred.Prompt = lipgloss.NewStyle().Foreground(theme.FgMuted)
	projectList.FilterInput.Styles.Focused.Prompt = lipgloss.NewStyle().Foreground(theme.Success)
	projectList.FilterInput.Styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(theme.FgMuted)

	projectList.SetShowFilter(true)
	projectList.Styles.TitleBar = projectList.Styles.TitleBar.Padding(0, 0, 0, 0)

	h := help.New()
	muted := lipgloss.NewStyle().Foreground(theme.FgMuted)
	halfMuted := lipgloss.NewStyle().Foreground(theme.FgHalfMuted)
	h.Styles.ShortKey = halfMuted
	h.Styles.ShortDesc = muted
	h.Styles.ShortSeparator = muted
	h.Styles.FullKey = halfMuted
	h.Styles.FullDesc = muted
	h.Styles.FullSeparator = muted

	// Load logs content for the center pager
	logsContent, _ := os.ReadFile("internal/cli/dashboardcmd/logs.log")

	return model{
		keys:      newKeyMap(),
		help:      h,
		list:      projectList,
		logsPager: components.NewLogsPager(),
	}.withLogs(string(logsContent))
}

func (m model) withLogs(content string) model {
	// Store content into pager at start; actual size will be set on first WindowSizeMsg
	m.logsPager.SetContent(content)
	return m
}

func (m model) Init() tea.Cmd {
	// v2: set terminal background color; Bubble Tea will reset on close.
	return tea.SetBackgroundColor(theme.BgBase)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, m.keys.CyclePane):
			m.activePane = (m.activePane + 1) % 2 // cycle between left(0) and center(1)
			return m, nil
		case key.Matches(msg, m.keys.ToggleHelp):
			m.help.ShowAll = !m.help.ShowAll
			// Recompute list size to account for new help height so the help bar
			// remains visible and columns shrink accordingly.
			helpHeight := 0
			if m.help.View(m.keys) != "" {
				helpHeight = lipgloss.Height(m.help.View(m.keys))
			}
			bodyHeight := m.height - helpHeight
			if bodyHeight < 1 {
				bodyHeight = 1
			}
			leftW, centerW, _ := computeColumnWidths(m.width)
			listWidth := leftW - totalHorizontalPadding
			listHeight := bodyHeight - 2 // header + spacing
			if listWidth < 1 {
				listWidth = 1
			}
			if listHeight < 1 {
				listHeight = 1
			}
			m.list.SetSize(listWidth, listHeight)
			// Also resize the center pager accounting for header (1) + spacer (1)
			m.logsPager.SetSize(centerW-(paddingHorizontal+1)*2, max(1, bodyHeight-2))
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Update list size based on new dimensions; reserve space for help bar even when hidden
		reservedHelpHeight := lipgloss.Height(m.renderHelp())
		if reservedHelpHeight == 0 {
			// Reserve at least one line to avoid content pushing help off-screen when toggled
			reservedHelpHeight = 1
		}
		bodyHeight := m.height - reservedHelpHeight
		if bodyHeight < 1 {
			bodyHeight = 1
		}
		leftW, centerW, _ := computeColumnWidths(m.width)

		// Set list size (account for padding and header)
		listWidth := leftW - totalHorizontalPadding
		// Subtract header (1) and margin below header (1)
		listHeight := bodyHeight - 2
		if listWidth < 1 {
			listWidth = 1
		}
		if listHeight < 1 {
			listHeight = 1
		}
		m.list.SetSize(listWidth, listHeight)
		// Size the center pager: subtract header (1) + spacer (1)
		m.logsPager.SetSize(centerW-(paddingHorizontal+1)*2, max(1, bodyHeight-2))
		return m, nil
	}

	// Update only the active pane so keystrokes are captured by one component
	var cmd2 tea.Cmd
	switch m.activePane {
	case 0:
		// Disable list's escape quit keybinding; keep only q/ctrl+c at app level
		m.list.DisableQuitKeybindings()
		m.list, cmd = m.list.Update(msg)
	case 1:
		m.logsPager, cmd2 = m.logsPager.Update(msg)
	}
	return m, tea.Batch(cmd, cmd2)
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

	// Always reserve a line for help; when collapsed, render an empty line of the same style
	if helpBar == "" {
		helpBar = lipgloss.NewStyle().Width(m.width).Render("")
	}
	content := lipgloss.JoinVertical(lipgloss.Left, columns, helpBar)

	// Fill the entire screen with the background color and render content.
	return lipgloss.NewStyle().
		Background(theme.BgBase).
		Width(m.width).
		Height(m.height).
		Render(content)
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
	leftStyle := box.Align(lipgloss.Left).Height(innerHeight).MaxHeight(innerHeight)
	centerStyle := box.Align(lipgloss.Left).Padding(0, paddingHorizontal+1).Height(innerHeight).MaxHeight(innerHeight)
	// Right column: clamp to available height to avoid pushing help off-screen
	rightStyle := box.Align(lipgloss.Left).Height(innerHeight).MaxHeight(innerHeight)

	// Titles (used for header titles)
	leftTitle := "Stacks"
	centerTitle := "Logs"

	// Compute widths for this frame
	leftW, centerW, _ := computeColumnWidths(m.width)

	// Left column: render list with header (active pane highlighted)
	var leftHeader string
	if m.activePane == 0 {
		leftHeader = components.RenderHeaderActive(leftTitle, leftW, totalHorizontalPadding)
	} else {
		leftHeader = renderHeaderWithPadding(leftTitle, leftW, totalHorizontalPadding)
	}
	leftContent := leftHeader + "\n" + m.list.View()

	// Center placeholder content with headers (headers sized to container content width)
	centerPadding := (paddingHorizontal + 1) * 2
	var centerHeader string
	if m.activePane == 1 {
		centerHeader = components.RenderHeaderActive(centerTitle, centerW, centerPadding)
	} else {
		centerHeader = renderHeaderWithPadding(centerTitle, centerW, centerPadding)
	}
	// Fit pager to available height in center column (header consumes one line)
	m.logsPager.SetSize(centerW-(paddingHorizontal+1)*2, max(1, innerHeight-1))
	centerContent := centerHeader + "\n\n" + m.logsPager.View()

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
	r1Header := renderHeaderWithPadding("Row 1", remainingContent, totalHorizontalPadding)
	r2Header := renderHeaderWithPadding("Row 2", remainingContent, totalHorizontalPadding)
	r3Header := renderHeaderWithPadding("Row 3", remainingContent, totalHorizontalPadding)
	rightRow1 := r1Header + "\n\n" + "placeholder"
	rightRow2 := r2Header + "\n\n" + "placeholder"
	rightRow3 := r3Header + "\n\n" + "placeholder"
	rightRows := lipgloss.JoinVertical(lipgloss.Left, rightRow1, rightRow2, rightRow3)
	rightView := rightStyle.Width(remainingContent).Render(rightRows)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftView, centerView, rightView)
}

func (m model) renderHelp() string {
	if m.width <= 0 {
		return m.help.View(m.keys)
	}
	// Expand help when '?' is toggled via k.ToggleHelp; we already flip m.help.ShowAll there
	return lipgloss.NewStyle().Width(m.width).Render(m.help.View(m.keys))
}

// renderHeader renders a single-line header like "◇ Title /////" that fills the full
// content width of the parent container, never wrapping. It clamps to the given width.
// The containerWidth should be the container's content width; the function accounts for
// horizontal padding internally.
// (renderHeader shim removed; use renderHeaderWithPadding instead)

// renderHeaderWithPadding allows callers to pass an explicit horizontal padding
// value so headers fill exactly the visible content width after padding changes.
func renderHeaderWithPadding(title string, containerWidth int, horizontalPadding int) string {
	return components.RenderHeader(title, containerWidth, horizontalPadding)
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
