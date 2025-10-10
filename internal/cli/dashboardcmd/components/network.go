package components

import (
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/theme"
)

// RenderNetwork renders a single-line network entry formatted as:
//
//	name - driver
//
// Styling follows the Simple component:
// - the prefix ("name - ") uses theme.FgHalfMuted
// - the driver uses theme.FgMuted and is italic
func RenderNetwork(name, driver string, highlight bool) string {
	_ = highlight
	nameStyle := lipgloss.NewStyle().Foreground(theme.FgHalfMuted)
	separator := lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Render(" - ")
	nameRendered := nameStyle.Render(name)
	drv := lipgloss.NewStyle().Foreground(theme.FgMuted).Italic(true).Render(driver)
	return nameRendered + separator + drv
}
