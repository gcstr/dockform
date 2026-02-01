package dashboardcmd

import (
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/components"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/theme"
)

// renderHelp renders the help bar at the bottom.
func (m model) renderHelp() string {
	base := m.help.View(m.keys)
	if m.width <= 0 {
		return base
	}
	return lipgloss.NewStyle().Width(m.width).Render(base)
}

// renderCommandPaletteWindow renders the command palette modal.
func (m model) renderCommandPaletteWindow() string {
	width := commandPaletteWidth(m.width)
	if available := max(1, m.width); width > available {
		width = available
	}
	innerWidth := max(1, width-2)
	contentWidth := commandListContentWidth(width)

	header := components.RenderHeaderActive("Commands", innerWidth, 0, "slash")

	containerName := strings.TrimSpace(m.selectedContainerName())
	if containerName == "" {
		containerName = "(no container selected)"
	}
	containerKey := lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Render("Container: ")
	containerValue := lipgloss.NewStyle().Foreground(theme.FgBase).Bold(true).Render(containerName)
	containerLine := lipgloss.NewStyle().
		Width(contentWidth).
		MaxWidth(contentWidth).
		Render(containerKey + containerValue)

	listView := lipgloss.NewStyle().
		Width(contentWidth).
		MaxWidth(contentWidth).
		Render(m.commandList.View())

	content := lipgloss.JoinVertical(lipgloss.Left, containerLine, "", listView)
	contentStyled := lipgloss.NewStyle().
		Padding(contentPaddingTop, contentPaddingRight, contentPaddingBottom, contentPaddingLeft).
		Width(innerWidth).
		Render(content)

	body := lipgloss.JoinVertical(lipgloss.Left, header, contentStyled)

	modal := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.Primary).
		Background(theme.BgBase).
		Width(width).
		Render(body)

	return modal
}

// renderHeaderWithPadding renders a header with padding.
func renderHeaderWithPadding(title string, containerWidth int, horizontalPadding int, pattern string) string {
	return components.RenderHeader(title, containerWidth, horizontalPadding, pattern)
}

// renderSlashBanner renders a banner with slashes.
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

// renderFilterPlaceholder renders a placeholder for the filter input.
func renderFilterPlaceholder(width int) string {
	text := "Press / to filter stacks"
	style := lipgloss.NewStyle().Foreground(theme.FgSubtle).Italic(true)
	if width > 0 {
		style = style.Width(width).MaxWidth(width)
	}
	return style.Render(text)
}

// renderSimpleWithWidth renders a key-value with width constraint.
func renderSimpleWithWidth(key, value string, totalWidth int) string {
	available := availableValueWidth(totalWidth, key)
	return components.RenderSimple(key, truncateRight(value, available))
}
