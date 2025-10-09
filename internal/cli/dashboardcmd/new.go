package dashboardcmd

import (
	"bufio"
	"context"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/buildinfo"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/components"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/data"
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

	// live state
	statusProvider *data.StatusProvider
	statusByKey    map[data.Key]data.Status
	logCancel      context.CancelFunc
	selectedName   string
	logsBuf        []string
	logLines       chan string
	// debounce
	pendingSelName string
	debounceTimer  *time.Timer

	quitting   bool
	activePane int
}

func stackItemsFromSummaries(summaries []data.StackSummary) []list.Item {
	items := make([]list.Item, 0)
	for _, summary := range summaries {
		if len(summary.Services) == 0 {
			items = append(items, components.StackItem{
				TitleText:     summary.Name,
				Service:       "",
				ContainerName: "",
				Containers:    []string{"(no services)"},
				Status:        "○ no services",
				FilterText:    summary.Name,
			})
			continue
		}
		for _, svc := range summary.Services {
			containers := presentServiceLines(svc)
			status := renderServiceStatus(svc)
			filter := buildFilterValue(summary.Name, svc)
			items = append(items, components.StackItem{
				TitleText:     summary.Name,
				Service:       svc.Service,
				ContainerName: svc.ContainerName,
				Containers:    containers,
				Status:        status,
				FilterText:    filter,
			})
		}
	}
	return items
}

func presentServiceLines(svc data.ServiceSummary) []string {
	// Only show service on the first line; rendering will substitute container name if known
	return []string{svc.Service, svc.Image}
}

func renderServiceStatus(_ data.ServiceSummary) string {
	return "○ status unknown"
}

func buildFilterValue(stackName string, svc data.ServiceSummary) string {
	parts := []string{}
	for _, piece := range []string{stackName, svc.Service, svc.ContainerName, svc.Image} {
		p := strings.TrimSpace(piece)
		if p == "" {
			continue
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, " ")
}

func newModel(stacks []data.StackSummary) model {
	items := stackItemsFromSummaries(stacks)
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

	return model{
		keys:        newKeyMap(),
		help:        h,
		list:        projectList,
		logsPager:   components.NewLogsPager(),
		statusByKey: make(map[data.Key]data.Status),
		logsBuf:     make([]string, 0, 512),
	}
}

// withLogs is no longer used; keep for reference if needed in future.
// func (m model) withLogs(content string) model {
//     m.logsPager.SetContent(content)
//     return m
// }

func (m model) Init() tea.Cmd {
	// v2: set terminal background color; Bubble Tea will reset on close.
	return tea.Batch(
		tea.SetBackgroundColor(theme.BgBase),
		m.tickStatuses(),
		m.tickLogs(),
		m.startInitialLogsCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case statusesMsg:
		// Merge statuses and update list item descriptions
		if m.statusByKey == nil {
			m.statusByKey = map[data.Key]data.Status{}
		}
		for k, v := range msg.statuses {
			m.statusByKey[k] = v
		}
		// Rebuild list items with updated status lines
		items := m.list.Items()
		newItems := make([]list.Item, 0, len(items))
		for _, it := range items {
			si, ok := it.(components.StackItem)
			if !ok {
				newItems = append(newItems, it)
				continue
			}
			key := data.Key{Stack: si.TitleText}
			// The key needs the service name; derive from first container line
			if si.Service != "" {
				key.Service = si.Service
			} else if len(si.Containers) > 0 {
				key.Service = si.Containers[0]
			}
			if st, ok := m.statusByKey[key]; ok {
				// Map state to color; build status line like "● Up ..."
				colorKey, text := data.FormatStatusLine(st.State, st.StatusText)
				prefix := ""
				switch colorKey {
				case "success":
					prefix = "[ok] "
				case "warning":
					prefix = "[warn] "
				default:
					prefix = "[err] "
				}
				si.Status = prefix + text
				// Update ContainerName to the live name so selection uses it
				if strings.TrimSpace(st.ContainerName) != "" {
					si.ContainerName = st.ContainerName
				}
			}
			newItems = append(newItems, si)
		}
		m.list.SetItems(newItems)
		return m, nil
	case statusTickMsg:
		return m, m.refreshStatusesCmd()
	case logsTickMsg:
		m = m.withFlushedLogs()
		return m, m.tickLogs()
	case startLogsFor:
		if strings.TrimSpace(msg.name) == "" {
			return m, nil
		}
		if m.pendingSelName != "" && msg.name != m.pendingSelName {
			return m, nil
		}
		if m.logCancel != nil {
			m.logCancel()
			m.logCancel = nil
		}
		m.selectedName = msg.name
		m.logsBuf = m.logsBuf[:0]
		m.logsPager.SetContent("")
		return m, m.streamLogsCmd(msg.name)
	case logStreamStartedMsg:
		// store cancel func for current stream
		m.logCancel = msg.cancel
		return m, nil
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
			// Account for a one-line top margin as well
			bodyHeight := m.height - helpHeight - 1
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
		// Account for a one-line top margin as well
		bodyHeight := m.height - reservedHelpHeight - 1
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
		// Size the center pager: subtract header (1) + spacer (1) + extra (1)
		m.logsPager.SetSize(centerW-(paddingHorizontal+1)*2, max(1, bodyHeight-3))
		return m, nil
	}

	// Update only the active pane so keystrokes are captured by one component
	var cmd2 tea.Cmd
	switch m.activePane {
	case 0:
		// Disable list's escape quit keybinding; keep only q/ctrl+c at app level
		m.list.DisableQuitKeybindings()
		oldIndex := m.list.Index()
		m.list, cmd = m.list.Update(msg)
		if m.list.Index() != oldIndex {
			// Debounce selection: delay starting logs to avoid rapid restarts during fast navigation
			it, _ := m.list.SelectedItem().(components.StackItem)
			m.pendingSelName = strings.TrimSpace(it.ContainerName)
			if m.debounceTimer != nil {
				m.debounceTimer.Stop()
			}
			name := m.pendingSelName
			m.debounceTimer = time.AfterFunc(200*time.Millisecond, func() {
				// Schedule a message to actually start logs for the pending selection
				m.logLines <- "" // wake up tick; we’ll rely on tick to refresh view
			})
			// Return a command to process the debounced selection in the next update cycle
			return m, func() tea.Msg {
				time.Sleep(220 * time.Millisecond)
				return startLogsFor{name: name}
			}
		}
	case 1:
		m.logsPager, cmd2 = m.logsPager.Update(msg)
	}
	return m, tea.Batch(cmd, cmd2)
}

// periodic status refresh
func (m model) tickStatuses() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return statusTickMsg{} })
}

func (m model) tickLogs() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return logsTickMsg{} })
}

type statusTickMsg struct{}
type logsTickMsg struct{}
type startLogsFor struct{ name string }

// startInitialLogsCmd schedules a one-shot start of logs for the initially selected item (if any)
func (m model) startInitialLogsCmd() tea.Cmd {
	return func() tea.Msg {
		it, ok := m.list.SelectedItem().(components.StackItem)
		if !ok {
			return nil
		}
		name := strings.TrimSpace(it.ContainerName)
		if name == "" {
			return nil
		}
		// Use same debounced path for consistency
		time.Sleep(150 * time.Millisecond)
		return startLogsFor{name: name}
	}
}

// refreshStatusesCmd queries docker for current statuses and updates model via a message
func (m model) refreshStatusesCmd() tea.Cmd {
	if m.statusProvider == nil {
		return m.tickStatuses()
	}
	// capture current items for background
	items := m.list.Items()
	return tea.Sequence(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			stackMap := map[string][]data.ServiceSummary{}
			for _, it := range items {
				si, ok := it.(components.StackItem)
				if !ok {
					continue
				}
				if len(si.Containers) == 0 {
					continue
				}
				service := si.Containers[0]
				cname := ""
				if strings.Contains(service, "(container:") {
					parts := strings.Split(service, "(container:")
					service = strings.TrimSpace(parts[0])
					seg := strings.TrimSpace(strings.TrimSuffix(parts[1], ")"))
					cname = seg
				}
				stackMap[si.TitleText] = append(stackMap[si.TitleText], data.ServiceSummary{Service: service, ContainerName: cname})
			}
			stacks := make([]data.StackSummary, 0, len(stackMap))
			for name, svcs := range stackMap {
				stacks = append(stacks, data.StackSummary{Name: name, Services: svcs})
			}
			statuses, err := m.statusProvider.FetchAll(ctx, stacks)
			if err == nil {
				return statusesMsg{statuses: statuses}
			}
			return nil
		},
		m.tickStatuses(),
	)
}

type statusesMsg struct{ statuses map[data.Key]data.Status }

// onSelectionChanged starts a logs stream for the newly selected item.
// onSelectionChanged was replaced by a debounced start using startLogsFor messages

// streamLogsCmd starts a docker logs --follow stream and feeds lines into the pager.
type logStreamStartedMsg struct{ cancel context.CancelFunc }

func (m *model) streamLogsCmd(name string) tea.Cmd {
	if m.statusProvider == nil {
		return nil
	}
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	// reader goroutine -> emit msgs to update the pager content
	if m.logLines == nil {
		m.logLines = make(chan string, 256)
	}
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			m.logLines <- sc.Text()
		}
	}()
	// writer command goroutine
	go func() {
		// Use underlying docker client via statusProvider
		_ = m.statusProvider.Docker().StreamContainerLogs(ctx, name, 300, "", pw)
		_ = pw.Close()
	}()
	return func() tea.Msg { return logStreamStartedMsg{cancel: cancel} }
}

func (m *model) withFlushedLogs() model {
	drained := false
	for m.logLines != nil {
		select {
		case ln := <-m.logLines:
			m.logsBuf = append(m.logsBuf, ln)
			if len(m.logsBuf) > 1000 {
				m.logsBuf = m.logsBuf[len(m.logsBuf)-1000:]
			}
			drained = true
		default:
			goto done
		}
	}
done:
	if drained {
		m.logsPager.SetContent(strings.Join(m.logsBuf, "\n"))
	}
	return *m
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
	// Account for a one-line top margin as well
	bodyHeight := m.height - helpHeight - 1
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	columns := m.renderColumns(bodyHeight)

	// Always reserve a line for help; when collapsed, render an empty line of the same style
	if helpBar == "" {
		helpBar = lipgloss.NewStyle().Width(m.width).Render("")
	}
	// Prepend one blank line as the top margin
	content := lipgloss.JoinVertical(lipgloss.Left, "", columns, helpBar)

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
		leftHeader = components.RenderHeaderActive(leftTitle, leftW, totalHorizontalPadding, "dash")
	} else {
		leftHeader = renderHeaderWithPadding(leftTitle, leftW, totalHorizontalPadding, "dash")
	}
	leftContent := leftHeader + "\n" + m.list.View()

	// Center placeholder content with headers (headers sized to container content width)
	centerPadding := (paddingHorizontal + 1) * 2
	var centerHeader string
	if m.activePane == 1 {
		centerHeader = components.RenderHeaderActive(centerTitle, centerW, centerPadding, "dash")
	} else {
		centerHeader = renderHeaderWithPadding(centerTitle, centerW, centerPadding, "dash")
	}
	// Fit pager to available height in center column, minus header (1), spacer (1), and one extra line
	m.logsPager.SetSize(centerW-(paddingHorizontal+1)*2, max(1, innerHeight-3))
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
	r2Header := renderHeaderWithPadding("Volumes", remainingContent, totalHorizontalPadding, "slash")
	r3Header := renderHeaderWithPadding("Networks", remainingContent, totalHorizontalPadding, "slash")

	// Top-right: gradient header "◇ Dockform /////" replacing the previous banner
	{
		contentWidth := remainingContent - totalHorizontalPadding
		if contentWidth < 1 {
			contentWidth = 1
		}
		base := "◇ Dockform "
		baseWidth := lipgloss.Width(base)
		slashCount := contentWidth - baseWidth
		var raw string
		if slashCount < 0 {
			// Clamp base to available width
			runes := []rune(base)
			if contentWidth < len(runes) {
				raw = string(runes[:contentWidth])
			} else {
				raw = base
			}
		} else {
			raw = base + strings.Repeat("╱", slashCount)
		}
		// Apply gradient from #5EC6F6 to #376FE9
		grad := components.RenderGradientText(raw, "#5EC6F6", "#376FE9")
		// Ensure the rendered line clamps to the visible width
		dockHeader := lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(grad)

		r1Line1 := components.RenderSimple("Context", "default")
		r1Line2 := components.RenderSimple("Host", "unix:///var/run/docker.sock")
		r1Line3 := components.RenderSimple("Version", buildinfo.Version())
		rightRow1 := dockHeader + "\n\n" + r1Line1 + "\n" + r1Line2 + "\n" + r1Line3 + "\n"

		// Assemble right column with updated top section
		v1 := components.RenderVolume("vaultwarden", "/mnt/data/vaultwarden", "1.2GB")
		v2 := components.RenderVolume("postgresql", "/var/lib/postgresql/data", "12.8GB")
		v3 := components.RenderVolume("redis", "/data", "512MB")
		rightRow2 := r2Header + "\n\n" + v1 + "\n\n" + v2 + "\n\n" + v3 + "\n"
		n1 := components.RenderNetwork("traefik", "bridge")
		n2 := components.RenderNetwork("frontend", "bridge")
		n3 := components.RenderNetwork("backend", "bridge")
		rightRow3 := r3Header + "\n\n" + n1 + "\n" + n2 + "\n" + n3 + "\n"
		rightRows := lipgloss.JoinVertical(lipgloss.Left, rightRow1, rightRow2, rightRow3)
		rightView := rightStyle.Width(remainingContent).Render(rightRows)

		return lipgloss.JoinHorizontal(lipgloss.Top, leftView, centerView, rightView)
	}
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
func renderHeaderWithPadding(title string, containerWidth int, horizontalPadding int, pattern string) string {
	return components.RenderHeader(title, containerWidth, horizontalPadding, pattern)
}

// renderSlashBanner builds a three-line banner with a centered middle line:
//
// ╱╱╱╱╱╱╱╱╱╱╱
// ╱╱╱ title ╱╱╱
// ╱╱╱╱╱╱╱╱╱╱╱
func renderSlashBanner(width int, title string) string {
	if width < 1 {
		width = 1
	}
	repeat := func(n int) string {
		if n < 0 {
			n = 0
		}
		return strings.Repeat("╱", n)
	}
	// Top and bottom full lines (Primary)
	slashStyle := lipgloss.NewStyle().Foreground(theme.Primary)
	top := slashStyle.Render(repeat(width))
	bottom := slashStyle.Render(repeat(width))

	// Middle: surround title with spaces and slashes; clamp to width
	rawCore := " " + title + " "
	coreStyled := lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Render(rawCore)
	coreW := lipgloss.Width(rawCore) // visible width (ignore ANSI codes)
	if coreW >= width {
		middle := lipgloss.NewStyle().Width(width).MaxWidth(width).Render(coreStyled)
		return top + "\n" + middle + "\n" + bottom
	}
	remain := width - coreW
	left := remain / 2
	right := remain - left
	leftSlashes := slashStyle.Render(repeat(left))
	rightSlashes := slashStyle.Render(repeat(right))
	middle := leftSlashes + coreStyled + rightSlashes
	return top + "\n" + middle + "\n" + bottom
}

// New creates the `dockform dashboard` command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Launch the Dockform dashboard (fullscreen TUI)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := common.SetupCLIContext(cmd)
			if err != nil {
				return err
			}

			loader, err := data.NewLoader(cliCtx.Config, cliCtx.Docker)
			if err != nil {
				return err
			}
			stacks, err := loader.StackSummaries(cliCtx.Ctx)
			if err != nil {
				return err
			}

			m := newModel(stacks)
			// wire status provider with identifier for filtering
			m.statusProvider = data.NewStatusProvider(cliCtx.Docker, cliCtx.Config.Docker.Identifier)

			p := tea.NewProgram(m, tea.WithAltScreen())
			_, err = p.Run()
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
