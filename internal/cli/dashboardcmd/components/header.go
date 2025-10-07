package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
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
		// If title is too long, truncate the whole thing
		headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
		return headerStyle.Width(contentWidth).MaxWidth(contentWidth).Render(base)
	}

	// Build slashes
	slashes := strings.Repeat("/", slashCount)
	result := base + slashes

	// Apply mid gray color to the entire header
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))

	// Force truncate at exact width to prevent any wrapping
	if lipgloss.Width(result) > contentWidth {
		// Truncate using lipgloss utilities
		return headerStyle.Width(contentWidth).MaxWidth(contentWidth).Render(result)
	}

	return headerStyle.Render(result)
}
