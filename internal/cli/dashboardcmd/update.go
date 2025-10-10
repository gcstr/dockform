package dashboardcmd

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/components"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/data"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/theme"
	"github.com/gcstr/dockform/internal/dockercli"
)

func (m model) Init() tea.Cmd {
	// v2: set terminal background color; Bubble Tea will reset on close.
	return tea.Batch(
		tea.SetBackgroundColor(theme.BgBase),
		m.tickStatuses(),
		m.tickLogs(),
		m.startInitialLogsCmd(),
		m.fetchDockerInfoCmd(),
		m.fetchVolumesCmd(),
		m.fetchNetworksCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case statusesMsg:
		if m.statusByKey == nil {
			m.statusByKey = map[data.Key]data.Status{}
		}
		for k, v := range msg.statuses {
			m.statusByKey[k] = v
		}
		items := m.list.Items()
		newItems := make([]list.Item, 0, len(items))
		for _, it := range items {
			si, ok := it.(components.StackItem)
			if !ok {
				newItems = append(newItems, it)
				continue
			}
			key := data.Key{Stack: si.TitleText}
			if si.Service != "" {
				key.Service = si.Service
			} else if len(si.Containers) > 0 {
				key.Service = si.Containers[0]
			}
			if st, ok := m.statusByKey[key]; ok {
				colorKey, text := data.FormatStatusLine(st.State, st.StatusText)
				si.StatusKind = colorKey
				si.Status = text
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
	case dockerInfoMsg:
		if strings.TrimSpace(msg.host) != "" {
			m.dockerHost = strings.TrimSpace(msg.host)
		}
		if strings.TrimSpace(msg.version) != "" {
			m.engineVersion = strings.TrimSpace(msg.version)
		}
		return m, nil
	case volumesMsg:
		if msg.volumes != nil {
			m.volumes = msg.volumes
		}
		return m, nil
	case networksMsg:
		if msg.networks != nil {
			m.networks = msg.networks
		}
		return m, nil
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
		m.logCancel = msg.cancel
		return m, nil
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true
			if m.logCancel != nil {
				m.logCancel()
				m.logCancel = nil
			}
			if m.debounceTimer != nil {
				m.debounceTimer.Stop()
				m.debounceTimer = nil
			}
			return m, tea.Quit
		case key.Matches(msg, m.keys.CyclePane):
			m.activePane = (m.activePane + 1) % 2
			return m, nil
		case key.Matches(msg, m.keys.ToggleHelp):
			m.help.ShowAll = !m.help.ShowAll
			helpHeight := 0
			if m.help.View(m.keys) != "" {
				helpHeight = lipgloss.Height(m.help.View(m.keys))
			}
			bodyHeight := m.height - helpHeight - 1
			if bodyHeight < 1 {
				bodyHeight = 1
			}
			leftW, centerW, _ := computeColumnWidths(m.width)
			listWidth := leftW - totalHorizontalPadding
			listHeight := bodyHeight - 2
			if listWidth < 1 {
				listWidth = 1
			}
			if listHeight < 1 {
				listHeight = 1
			}
			m.list.SetSize(listWidth, listHeight)
			m.logsPager.SetSize(centerW-(paddingHorizontal+1)*2, max(1, bodyHeight-2))
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		reservedHelpHeight := lipgloss.Height(m.renderHelp())
		if reservedHelpHeight == 0 {
			reservedHelpHeight = 1
		}
		bodyHeight := m.height - reservedHelpHeight - 1
		if bodyHeight < 1 {
			bodyHeight = 1
		}
		leftW, centerW, _ := computeColumnWidths(m.width)

		listWidth := leftW - totalHorizontalPadding
		listHeight := bodyHeight - 2
		if listWidth < 1 {
			listWidth = 1
		}
		if listHeight < 1 {
			listHeight = 1
		}
		m.list.SetSize(listWidth, listHeight)
		m.logsPager.SetSize(centerW-(paddingHorizontal+1)*2, max(1, bodyHeight-3))
		return m, nil
	}

	var cmd2 tea.Cmd
	switch m.activePane {
	case 0:
		m.list.DisableQuitKeybindings()
		oldIndex := m.list.Index()
		m.list, cmd = m.list.Update(msg)
		if m.list.Index() != oldIndex {
			it, _ := m.list.SelectedItem().(components.StackItem)
			m.pendingSelName = strings.TrimSpace(it.ContainerName)
			name := m.pendingSelName
			return m, func() tea.Msg {
				time.Sleep(200 * time.Millisecond)
				return startLogsFor{name: name}
			}
		}
	case 1:
		m.logsPager, cmd2 = m.logsPager.Update(msg)
	}
	return m, tea.Batch(cmd, cmd2)
}

func (m model) tickStatuses() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return statusTickMsg{} })
}

type statusTickMsg struct{}
type statusesMsg struct{ statuses map[data.Key]data.Status }
type dockerInfoMsg struct {
	host    string
	version string
}
type volumesMsg struct {
	volumes []dockercli.VolumeSummary
}
type networksMsg struct {
	networks []dockercli.NetworkSummary
}

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
		time.Sleep(150 * time.Millisecond)
		return startLogsFor{name: name}
	}
}

func (m model) refreshStatusesCmd() tea.Cmd {
	if m.statusProvider == nil {
		return m.tickStatuses()
	}
	stacks := m.stacks
	return tea.Sequence(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			statuses, err := m.statusProvider.FetchAll(ctx, stacks)
			if err == nil {
				return statusesMsg{statuses: statuses}
			}
			return nil
		},
		m.tickStatuses(),
	)
}

func (m model) fetchDockerInfoCmd() tea.Cmd {
	if m.dockerClient == nil {
		return nil
	}
	ctx := m.ctx
	return func() tea.Msg {
		host, _ := m.dockerClient.ContextHost(ctx)
		version, _ := m.dockerClient.ServerVersion(ctx)
		return dockerInfoMsg{host: strings.TrimSpace(host), version: strings.TrimSpace(version)}
	}
}

func (m model) fetchVolumesCmd() tea.Cmd {
	if m.dockerClient == nil {
		return nil
	}
	ctx := m.ctx
	return func() tea.Msg {
		vols, err := m.dockerClient.VolumeSummaries(ctx)
		if err != nil {
			return nil
		}
		return volumesMsg{volumes: vols}
	}
}

func (m model) fetchNetworksCmd() tea.Cmd {
	if m.dockerClient == nil {
		return nil
	}
	ctx := m.ctx
	return func() tea.Msg {
		nets, err := m.dockerClient.NetworkSummaries(ctx)
		if err != nil {
			return nil
		}
		return networksMsg{networks: nets}
	}
}
