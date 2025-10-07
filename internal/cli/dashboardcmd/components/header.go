package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/theme"
)

// RenderHeader renders a single-line header like "◇ Title /////" that fills the full
// content width of the parent container, never wrapping. It clamps to the given width.
// The containerWidth should be the container's content width; the function accounts for
// horizontal padding via the totalHorizontalPadding value passed by the caller.
func RenderHeader(title string, containerWidth int, totalHorizontalPadding int) string {
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
		// If title is too long, truncate the whole thing, style the title
		baseStyled := lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Render(base)
		return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(baseStyled)
	}

	// Build slashes
	slashes := strings.Repeat("/", slashCount)
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
