package dashboardcmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/theme"
)

const (
	commandPaletteMinWidth = 34
	commandPaletteMaxWidth = 64

	contentPaddingLeft   = 2
	contentPaddingRight  = 2
	contentPaddingTop    = 1
	contentPaddingBottom = 1
)

// commandItem represents an actionable entry in the command palette.
type commandItem struct {
	id    string
	title string
}

func (i commandItem) Title() string       { return i.title }
func (i commandItem) Description() string { return "" }
func (i commandItem) FilterValue() string { return strings.ToLower(i.title) }

// commandDelegate renders the command palette entries similar to the
// Bubble Tea list-simple example (arrow prefix on selection).
type commandDelegate struct{}

func (d commandDelegate) Height() int                               { return 1 }
func (d commandDelegate) Spacing() int                              { return 0 }
func (d commandDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }

func (d commandDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ci, ok := item.(commandItem)
	if !ok {
		return
	}

	normalPrefix := lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Render("* ")
	selectedPrefix := lipgloss.NewStyle().Foreground(theme.Primary).Render("> ")
	normalTitle := lipgloss.NewStyle().Foreground(theme.FgHalfMuted)
	selectedTitle := lipgloss.NewStyle().Foreground(theme.FgBase).Bold(true)

	var rendered string
	width := m.Width()
	if index == m.Index() {
		line := selectedPrefix + selectedTitle.Render(ci.title)
		if width > 0 {
			rendered = lipgloss.NewStyle().Width(width).MaxWidth(width).Render(line)
		} else {
			rendered = line
		}
	} else {
		line := normalPrefix + normalTitle.Render(ci.title)
		if width > 0 {
			rendered = lipgloss.NewStyle().Width(width).MaxWidth(width).Render(line)
		} else {
			rendered = line
		}
	}
	_, _ = fmt.Fprint(w, rendered)
}

func newCommandPalette() list.Model {
	items := []list.Item{
		commandItem{id: "pause", title: "Pause"},
		commandItem{id: "restart", title: "Restart"},
		commandItem{id: "stop", title: "Stop"},
		commandItem{id: "delete", title: "Delete"},
	}

	delegate := commandDelegate{}
	l := list.New(items, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()
	l.SetSize(commandListContentWidth(commandPaletteMinWidth), len(items))
	return l
}

func commandPaletteWidth(total int) int {
	if total <= 0 {
		return commandPaletteMinWidth
	}
	margin := 6
	width := total - margin
	if width < commandPaletteMinWidth {
		width = total - 2
	}
	if width < 1 {
		width = 1
	}
	if width > commandPaletteMaxWidth {
		width = commandPaletteMaxWidth
	}
	if width > total {
		width = total
	}
	minContent := contentPaddingLeft + contentPaddingRight + 1
	if width < minContent {
		width = min(total, minContent)
		if width < 1 {
			width = 1
		}
	}
	return width
}

func commandListContentWidth(paletteWidth int) int {
	inner := max(1, paletteWidth-2) // account for borders
	width := inner - (contentPaddingLeft + contentPaddingRight)
	if width < 1 {
		return 1
	}
	return width
}
