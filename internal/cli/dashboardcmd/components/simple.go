package components

import (
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/theme"
)

// RenderSimple renders a single line in the format "key: value" with styles:
// - the "key: " part uses theme.FgHalfMuted
// - the value uses theme.FgMuted and is italic
// It returns the styled string; callers decide placement and width concerns.
func RenderSimple(key, value string) string {
	keyStyled := lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Render(key + ": ")
	valueStyled := lipgloss.NewStyle().Foreground(theme.FgMuted).Italic(true).Render(value)
	return keyStyled + valueStyled
}
