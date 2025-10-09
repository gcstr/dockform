package dashboardcmd

import (
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/buildinfo"
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
	return lipgloss.NewStyle().
		Background(theme.BgBase).
		Width(m.width).
		Height(m.height).
		Render(content)
}

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
	leftContent := leftHeader + "\n" + m.list.View()

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
		grad := components.RenderGradientText(raw, "#5EC6F6", "#376FE9")
		return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(grad)
	}
	r1Header := buildGradHeader("Docker")
	r2Header := buildGradHeader("Volumes")
	r3Header := buildGradHeader("Networks")

	r0Line0 := components.RenderGradientText("DOCKFORM v0.6.3", "#5EC6F6", "#376FE9")
	r0Line1 := components.RenderSimple("Identifier", "homelab")
	r0Line2 := components.RenderSimple("Manifest", ".../dockform/manifest.yaml")
	rightRow0 := r0Line0 + "\n\n" + r0Line1 + "\n" + r0Line2 + "\n"

	r1Line1 := components.RenderSimple("Context", "default")
	r1Line2 := components.RenderSimple("Host", "unix:///var/run/docker.sock")
	r1Line3 := components.RenderSimple("Version", buildinfo.Version())
	rightRow1 := r1Header + "\n\n" + r1Line1 + "\n" + r1Line2 + "\n" + r1Line3 + "\n"

	v1 := components.RenderVolume("vaultwarden", "/mnt/data/vaultwarden", "1.2GB")
	v2 := components.RenderVolume("postgresql", "/var/lib/postgresql/data", "12.8GB")
	v3 := components.RenderVolume("redis", "/data", "512MB")
	rightRow2 := r2Header + "\n\n" + v1 + "\n\n" + v2 + "\n\n" + v3 + "\n"
	n1 := components.RenderNetwork("traefik", "bridge")
	n2 := components.RenderNetwork("frontend", "bridge")
	n3 := components.RenderNetwork("backend", "bridge")
	rightRow3 := r3Header + "\n\n" + n1 + "\n" + n2 + "\n" + n3 + "\n"
	rightRows := lipgloss.JoinVertical(lipgloss.Left, rightRow0, rightRow1, rightRow2, rightRow3)
	rightView := rightStyle.Width(remainingContent).Render(rightRows)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftView, centerView, rightView)
}

func (m model) renderHelp() string {
	if m.width <= 0 {
		return m.help.View(m.keys)
	}
	return lipgloss.NewStyle().Width(m.width).Render(m.help.View(m.keys))
}

func renderHeaderWithPadding(title string, containerWidth int, horizontalPadding int, pattern string) string {
	return components.RenderHeader(title, containerWidth, horizontalPadding, pattern)
}

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
	slashStyle := lipgloss.NewStyle().Foreground(theme.Primary)
	top := slashStyle.Render(repeat(width))
	bottom := slashStyle.Render(repeat(width))

	rawCore := " " + title + " "
	coreStyled := lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Render(rawCore)
	coreW := lipgloss.Width(rawCore)
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
