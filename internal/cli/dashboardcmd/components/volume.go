package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/theme"
)

// RenderVolume renders a simple volume block with the following format:
//
//	name
//	├ /mnt/path
//	└ local
//
// Colors follow the same pattern used in stack list items: title and tree in
// theme.FgBase; secondary text in theme.FgHalfMuted; the final line is italic.
func RenderVolume(name, mountPath, detail string, highlight bool) string {
	_ = highlight
	titleStyle := lipgloss.NewStyle().Foreground(theme.FgBase).Bold(true)
	treeStyle := lipgloss.NewStyle().Foreground(theme.FgBase)
	textStyle := lipgloss.NewStyle().Foreground(theme.FgHalfMuted)
	textItalicStyle := textStyle.Italic(true)

	var lines []string
	lines = append(lines, treeStyle.Render("")+titleStyle.Render(name))
	lines = append(lines, treeStyle.Render("├ ")+textStyle.Render(mountPath))
	lines = append(lines, treeStyle.Render("└ ")+textItalicStyle.Render(detail))
	return strings.Join(lines, "\n")
}
