package dashboardcmd

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/list"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/components"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/data"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/theme"
	"github.com/gcstr/dockform/internal/dockercli"
)

// model is the Bubble Tea model for the dashboard.
type model struct {
	width  int
	height int

	version       string
	identifier    string
	contextName   string
	dockerHost    string
	engineVersion string
	manifestPath  string

	ctx          context.Context
	dockerClient *dockercli.Client
	stacks       []data.StackSummary
	volumes      []dockercli.VolumeSummary
	networks     []dockercli.NetworkSummary

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

	// cached UI strings by key (e.g., right-column gradient headers)
	headerCache map[string]string

	quitting   bool
	activePane int
}

func newModel(ctx context.Context, docker *dockercli.Client, stacks []data.StackSummary, version, identifier, manifestPath, contextName, dockerHost, engineVersion string) model {
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
		version:       strings.TrimSpace(version),
		identifier:    strings.TrimSpace(identifier),
		contextName:   strings.TrimSpace(contextName),
		dockerHost:    strings.TrimSpace(dockerHost),
		engineVersion: strings.TrimSpace(engineVersion),
		manifestPath:  strings.TrimSpace(manifestPath),
		ctx:           ctx,
		dockerClient:  docker,
		stacks:        stacks,
		volumes:       nil,
		networks:      nil,
		keys:          newKeyMap(),
		help:          h,
		list:          projectList,
		logsPager:     components.NewLogsPager(),
		statusByKey:   make(map[data.Key]data.Status),
		logsBuf:       make([]string, 0, 512),
		headerCache:   make(map[string]string),
	}
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
