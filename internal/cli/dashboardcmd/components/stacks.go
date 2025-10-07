package components

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/theme"
)

// StackItem represents an item in the stacks list (formerly projectItem).
type StackItem struct {
	TitleText  string
	Containers []string
	Status     string
}

func (i StackItem) Title() string       { return i.TitleText }
func (i StackItem) Description() string { return i.Status }
func (i StackItem) FilterValue() string { return i.TitleText }

// StacksDelegate renders stack items with tree-like formatting.
type StacksDelegate struct{}

func (d StacksDelegate) Height() int                               { return 4 }
func (d StacksDelegate) Spacing() int                              { return 1 }
func (d StacksDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }

func (d StacksDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i, ok := item.(StackItem)
	if !ok {
		return
	}

	var lines []string

	// Base (unselected) styles
	titleStyle := lipgloss.NewStyle().Foreground(theme.FgBase).Bold(true)
	treeStyle := lipgloss.NewStyle().Foreground(theme.FgBase)
	textStyle := lipgloss.NewStyle().Foreground(theme.FgHalfMuted)
	textItalicStyle := textStyle.Italic(true)
	greenBullet := lipgloss.NewStyle().Foreground(theme.Success).Render("●")

	// Reduced indentation
	lines = append(lines, treeStyle.Render("")+titleStyle.Render(i.TitleText))

	for idx, container := range i.Containers {
		rendered := textStyle.Render(container)
		if idx == 1 {
			rendered = textItalicStyle.Render(container)
		}
		lines = append(lines, treeStyle.Render("├ ")+rendered)
	}

	statusText := i.Status
	if strings.HasPrefix(statusText, "●") || strings.HasPrefix(statusText, "○") {
		remainder := strings.TrimSpace(strings.TrimLeft(statusText, "●○ "))
		statusText = greenBullet + " " + textItalicStyle.Render(remainder)
	} else {
		statusText = greenBullet + " " + textItalicStyle.Render(statusText)
	}
	lines = append(lines, treeStyle.Render("└ ")+statusText)

	block := strings.Join(lines, "\n")
	if index == m.Index() {
		// Selected state: all text becomes FgBase, and the title becomes Primary
		selectedTree := lipgloss.NewStyle().Foreground(theme.FgBase)
		selectedText := lipgloss.NewStyle().Foreground(theme.FgBase)
		selectedItalic := selectedText.Italic(true)
		selectedTitle := lipgloss.NewStyle().Foreground(theme.Primary).Bold(true)

		var selectedLines []string
		// title
		selectedLines = append(selectedLines, selectedTree.Render("")+selectedTitle.Render(i.TitleText))
		// containers
		for idx, container := range i.Containers {
			rendered := selectedText.Render(container)
			if idx == 1 {
				rendered = selectedItalic.Render(container)
			}
			selectedLines = append(selectedLines, selectedTree.Render("├ ")+rendered)
		}
		// status
		statusText := i.Status
		if strings.HasPrefix(statusText, "●") || strings.HasPrefix(statusText, "○") {
			remainder := strings.TrimSpace(strings.TrimLeft(statusText, "●○ "))
			statusText = selectedText.Render(remainder)
		} else {
			statusText = selectedText.Render(statusText)
		}
		// Keep the bullet green for status while the text is FgBase
		greenBullet := lipgloss.NewStyle().Foreground(theme.Success).Render("●")
		selectedLines = append(selectedLines, selectedTree.Render("└ ")+greenBullet+" "+selectedItalic.Render(statusText))

		block = lipgloss.NewStyle().Bold(true).Render(strings.Join(selectedLines, "\n"))
	}

	_, _ = fmt.Fprint(w, block)
}
