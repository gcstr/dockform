package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/theme"
)

func patternChar(pattern string) string {
	switch pattern {
	case "slash":
		return "╱"
	case "dash":
		return "━"
	default:
		return pattern
	}
}

// RenderHeader renders a single-line header like "◇ Title /////" that fills the full
// content width of the parent container, never wrapping. It clamps to the given width.
// The containerWidth should be the container's content width; the function accounts for
// horizontal padding via the totalHorizontalPadding value passed by the caller.
func RenderHeader(title string, containerWidth int, totalHorizontalPadding int, pattern string) string {
	pc := patternChar(pattern)

	// Account for horizontal padding inside the container
	contentWidth := containerWidth - totalHorizontalPadding
	if contentWidth <= 0 {
		return ""
	}
	// If no title, fill the entire line with the pattern character
	if strings.TrimSpace(title) == "" {
		slashes := strings.Repeat(pc, contentWidth)
		slashesStyled := lipgloss.NewStyle().Foreground(theme.FgSubtle).Render(slashes)
		if lipgloss.Width(slashesStyled) > contentWidth {
			return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(slashesStyled)
		}
		return slashesStyled
	}
	// Build the base: "◇ Title "
	base := fmt.Sprintf("◇ %s ", title)
	baseWidth := lipgloss.Width(base)

	// Calculate how many slashes we need to fill the exact width
	slashCount := contentWidth - baseWidth
	if slashCount < 0 {
		// If title is too long, truncate the whole thing, style the title
		baseStyled := lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Render(base)
		return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(baseStyled)
	}

	// Build slashes
	slashes := strings.Repeat(pc, slashCount)
	// Style parts separately
	baseStyled := lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Render(base)
	slashesStyled := lipgloss.NewStyle().Foreground(theme.FgSubtle).Render(slashes)
	result := baseStyled + slashesStyled

	// Force truncate at exact width to prevent any wrapping
	if lipgloss.Width(result) > contentWidth {
		// Truncate using lipgloss utilities
		return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(result)
	}

	return result
}

// RenderHeaderActive renders the same header but highlights the title section
// with the primary color to denote focus/selection.
func RenderHeaderActive(title string, containerWidth int, totalHorizontalPadding int, pattern string) string {
	pc := patternChar(pattern)

	// Account for horizontal padding inside the container
	contentWidth := containerWidth - totalHorizontalPadding
	if contentWidth <= 0 {
		return ""
	}
	// If no title, fill the entire line with the pattern character using gradient
	if strings.TrimSpace(title) == "" {
		raw := strings.Repeat(pc, contentWidth)
		grad := RenderGradientText(raw, "#5EC6F6", "#376FE9")
		if lipgloss.Width(grad) > contentWidth {
			return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(grad)
		}
		return grad
	}
	// Build the base: "◇ Title "
	base := fmt.Sprintf("◇ %s ", title)
	baseWidth := lipgloss.Width(base)

	// Calculate how many slashes we need to fill the exact width
	slashCount := contentWidth - baseWidth
	if slashCount < 0 {
		// If title is too long, truncate then apply gradient across visible runes
		runes := []rune(base)
		raw := base
		if contentWidth < len(runes) {
			raw = string(runes[:contentWidth])
		}
		grad := RenderGradientText(raw, "#5EC6F6", "#376FE9")
		return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(grad)
	}

	// Build slashes
	slashes := strings.Repeat(pc, slashCount)
	// Apply gradient across entire header text
	raw := base + slashes
	result := RenderGradientText(raw, "#5EC6F6", "#376FE9")

	// Force truncate at exact width to prevent any wrapping
	if lipgloss.Width(result) > contentWidth {
		// Truncate using lipgloss utilities
		return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(result)
	}

	return result
}
