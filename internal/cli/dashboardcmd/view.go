package dashboardcmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/v2/list"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/components"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/theme"
)

const (
	leftMinWidth   = 30
	leftMaxWidth   = 40
	rightMinWidth  = 20
	rightMaxWidth  = 30
	centerMinWidth = 40

	paddingVertical        = 0
	paddingHorizontal      = 1
	totalHorizontalPadding = paddingHorizontal * 2

	leftOverhead   = 2
	centerOverhead = 4
	rightOverhead  = 2
)

// View renders the dashboard UI.
func (m model) View() string {
	if m.quitting {
		return ""
	}
	helpBar := m.renderHelp()
	helpHeight := 0
	if helpBar != "" {
		helpHeight = lipgloss.Height(helpBar)
	}
	bodyHeight := m.height - helpHeight - 1
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	columns := m.renderColumns(bodyHeight)
	if helpBar == "" {
		helpBar = lipgloss.NewStyle().Width(m.width).Render("")
	}
	content := lipgloss.JoinVertical(lipgloss.Left, "", columns, helpBar)
	base := lipgloss.NewStyle().
		Background(theme.BgBase).
		Width(m.width).
		Height(m.height).
		Render(content)
	if !m.commandPaletteOpen {
		return base
	}

	baseLayer := lipgloss.NewLayer(base).ID("base")
	canvas := lipgloss.NewCanvas(baseLayer)

	palette := m.renderCommandPaletteWindow()
	paletteLayer := lipgloss.NewLayer(palette).ID("command_palette")
	pWidth := lipgloss.Width(palette)
	pHeight := lipgloss.Height(palette)
	if pWidth > m.width {
		pWidth = m.width
	}
	if pHeight > m.height {
		pHeight = m.height
	}
	x := max(0, (m.width-pWidth)/2)
	y := max(0, (m.height-pHeight)/2)
	paletteLayer.X(x).Y(y).Z(1)
	canvas.AddLayers(paletteLayer)

	return canvas.Render()
}

// computeColumnWidths calculates the width distribution for the three columns.
func computeColumnWidths(total int) (left, center, right int) {
	if total <= 0 {
		return 1, 1, 1
	}
	usable := total - (leftOverhead + centerOverhead + rightOverhead)
	if usable < 3 {
		return 1, 1, 1
	}
	left = leftMinWidth
	right = rightMinWidth
	center = usable - left - right
	if center < 1 {
		center = 1
		deficit := (left + right + center) - usable
		if deficit > 0 {
			squeezeLeft := min(deficit/2+deficit%2, left-1)
			left -= squeezeLeft
			deficit -= squeezeLeft
			squeezeRight := min(deficit, right-1)
			right -= squeezeRight
		}
		return left, center, right
	}
	minSum := leftMinWidth + rightMinWidth + centerMinWidth
	extra := usable - minSum
	if extra <= 0 {
		return left, max(1, center), right
	}
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

// renderColumns renders the three-column layout.
func (m model) renderColumns(bodyHeight int) string {
	box := lipgloss.NewStyle().Padding(paddingVertical, paddingHorizontal)
	innerHeight := bodyHeight
	if innerHeight < 0 {
		innerHeight = 0
	}
	leftStyle := box.Align(lipgloss.Left).Height(innerHeight).MaxHeight(innerHeight)
	centerStyle := box.Align(lipgloss.Left).Padding(0, paddingHorizontal+1).Height(innerHeight).MaxHeight(innerHeight)
	rightStyle := box.Align(lipgloss.Left).Height(innerHeight).MaxHeight(innerHeight)

	leftTitle := "Stacks"
	centerTitle := "Logs"

	leftW, centerW, _ := computeColumnWidths(m.width)

	var leftHeader string
	if m.activePane == 0 {
		leftHeader = components.RenderHeaderActive(leftTitle, leftW, totalHorizontalPadding, "dash")
	} else {
		leftHeader = renderHeaderWithPadding(leftTitle, leftW, totalHorizontalPadding, "dash")
	}
	listView := m.list.View()
	if m.list.FilterState() == list.Filtering {
		if idx := strings.Index(listView, "\n"); idx != -1 {
			listView = listView[:idx+1] + "\n" + listView[idx+1:]
		} else {
			listView += "\n"
		}
	}
	if m.list.FilterState() == list.Unfiltered {
		placeholder := renderFilterPlaceholder(leftW - totalHorizontalPadding)
		if placeholder != "" {
			listView = placeholder + "\n" + listView
		}
	}
	leftContent := leftHeader + "\n" + listView

	centerPadding := (paddingHorizontal + 1) * 2
	var centerHeader string
	if m.activePane == 1 {
		centerHeader = components.RenderHeaderActive(centerTitle, centerW, centerPadding, "dash")
	} else {
		centerHeader = renderHeaderWithPadding(centerTitle, centerW, centerPadding, "dash")
	}
	m.logsPager.SetSize(centerW-(paddingHorizontal+1)*2, max(1, innerHeight-3))
	centerContent := centerHeader + "\n\n" + m.logsPager.View()

	leftView := leftStyle.Width(leftW).Render(leftContent)
	centerView := centerStyle.Width(centerW).Render(centerContent)

	used := lipgloss.Width(lipgloss.JoinHorizontal(lipgloss.Top, leftView, centerView))
	remaining := m.width - used
	remainingContent := remaining - rightOverhead + 2
	if remainingContent < 1 {
		remainingContent = 1
	}
	contentWidth := remainingContent - totalHorizontalPadding
	if contentWidth < 1 {
		contentWidth = 1
	}
	buildGradHeader := func(title string) string {
		cacheKey := fmt.Sprintf("hdr:%s:%d", title, contentWidth)
		if m.headerCache != nil {
			if s, ok := m.headerCache[cacheKey]; ok {
				return s
			}
		}
		base := "◇ " + title + " "
		baseWidth := lipgloss.Width(base)
		slashCount := contentWidth - baseWidth
		var raw string
		if slashCount < 0 {
			runes := []rune(base)
			if contentWidth < len(runes) {
				raw = string(runes[:contentWidth])
			} else {
				raw = base
			}
		} else {
			raw = base + strings.Repeat("╱", slashCount)
		}
		grad := components.RenderThemeGradient(raw)
		rendered := lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(grad)
		if m.headerCache != nil {
			m.headerCache[cacheKey] = rendered
		}
		return rendered
	}
	r1Header := buildGradHeader("Docker")
	r2Header := buildGradHeader("Volumes")
	r3Header := buildGradHeader("Networks")

	versionLabel := fmt.Sprintf("DOCKFORM %s", displayVersion(m.version))
	r0Line0 := components.RenderThemeGradient(versionLabel)
	r0Line1 := renderSimpleWithWidth("Identifier", displayIdentifier(m.identifier), contentWidth)
	manifestDisplay := truncateLeft(displayManifestPath(m.manifestPath), availableValueWidth(contentWidth, "Manifest"))
	r0Line2 := components.RenderSimple("Manifest", manifestDisplay)
	rightRow0 := r0Line0 + "\n\n" + r0Line1 + "\n" + r0Line2 + "\n"

	r1Line1 := renderSimpleWithWidth("Context", displayContextName(m.contextName), contentWidth)
	r1Line2 := renderSimpleWithWidth("Host", displayDockerHost(m.dockerHost), contentWidth)
	r1Line3 := renderSimpleWithWidth("Version", displayEngineVersion(m.engineVersion), contentWidth)
	rightRow1 := r1Header + "\n\n" + r1Line1 + "\n" + r1Line2 + "\n" + r1Line3 + "\n"

	volumesBlock := m.renderVolumesSection(contentWidth)
	rightRow2 := r2Header + "\n\n" + volumesBlock + "\n"
	networksBlock := m.renderNetworksSection(contentWidth)
	rightRow3 := r3Header + "\n\n" + networksBlock + "\n"
	rightRows := lipgloss.JoinVertical(lipgloss.Left, rightRow0, rightRow1, rightRow2, rightRow3)
	rightView := rightStyle.Width(remainingContent).Render(rightRows)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftView, centerView, rightView)
}

// renderVolumesSection renders the volumes panel content.
func (m model) renderVolumesSection(contentWidth int) string {
	active := m.selectedVolumeSet()
	if len(active) == 0 {
		return lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Italic(true).Render("(no volumes attached)")
	}
	blocks := make([]string, 0, len(active))
	lineWidth := contentWidth - 2
	if lineWidth < 1 {
		lineWidth = 1
	}
	for _, vol := range m.volumes {
		mount := truncateRight(displayVolumeMount(vol.Mountpoint), lineWidth)
		driver := truncateRight(displayVolumeDriver(vol.Driver), lineWidth)
		nameKey := strings.TrimSpace(vol.Name)
		if _, ok := active[nameKey]; !ok {
			continue
		}
		blocks = append(blocks, components.RenderVolume(vol.Name, mount, driver, true))
	}
	if len(blocks) == 0 {
		return lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Italic(true).Render("(no volumes attached)")
	}
	return strings.Join(blocks, "\n\n")
}

// renderNetworksSection renders the networks panel content.
func (m model) renderNetworksSection(contentWidth int) string {
	active := m.selectedNetworkSet()
	if len(active) == 0 {
		return lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Italic(true).Render("(no networks attached)")
	}
	lines := make([]string, 0, len(active))
	for _, n := range m.networks {
		name := truncateRight(n.Name, contentWidth)
		driver := truncateRight(displayNetworkDriver(n.Driver), contentWidth-lipgloss.Width(name)-3)
		nameKey := strings.TrimSpace(n.Name)
		if _, ok := active[nameKey]; !ok {
			continue
		}
		lines = append(lines, components.RenderNetwork(name, driver, true))
	}
	if len(lines) == 0 {
		return lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Italic(true).Render("(no networks attached)")
	}
	return strings.Join(lines, "\n")
}
