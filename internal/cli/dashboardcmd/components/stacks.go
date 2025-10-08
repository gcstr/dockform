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
	FilterText string
}

func (i StackItem) Title() string       { return i.TitleText }
func (i StackItem) Description() string { return i.Status }
func (i StackItem) FilterValue() string {
	if i.FilterText != "" {
		return i.FilterText
	}
	return i.TitleText
}

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

	var bodyLines []string

	// Base (unselected) styles
	titleStyle := lipgloss.NewStyle().Foreground(theme.FgBase).Bold(true)
	treeStyle := lipgloss.NewStyle().Foreground(theme.FgBase)
	textStyle := lipgloss.NewStyle().Foreground(theme.FgHalfMuted)
	textItalicStyle := textStyle.Italic(true)
	greenBullet := lipgloss.NewStyle().Foreground(theme.Success).Render("●")

	// Reduced indentation
	bodyLines = append(bodyLines, treeStyle.Render("")+titleStyle.Render(i.TitleText))

	for idx, container := range i.Containers {
		rendered := textStyle.Render(container)
		if idx%2 == 1 {
			rendered = textItalicStyle.Render(container)
		}
		bodyLines = append(bodyLines, treeStyle.Render("├ ")+rendered)
	}

	statusText := i.Status
	if strings.HasPrefix(statusText, "●") || strings.HasPrefix(statusText, "○") {
		remainder := strings.TrimSpace(strings.TrimLeft(statusText, "●○ "))
		statusText = greenBullet + " " + textItalicStyle.Render(remainder)
	} else {
		statusText = greenBullet + " " + textItalicStyle.Render(statusText)
	}
	renderedStatus := treeStyle.Render("└ ") + statusText

	width := m.Width()
	lines := fitLinesToHeight(bodyLines, renderedStatus, d.Height(), width)
	block := strings.Join(lines, "\n")
	if index == m.Index() {
		// Selected state: all text becomes FgBase, and the title becomes Primary
		selectedTree := lipgloss.NewStyle().Foreground(theme.FgBase)
		selectedText := lipgloss.NewStyle().Foreground(theme.FgBase)
		selectedItalic := selectedText.Italic(true)
		selectedTitle := lipgloss.NewStyle().Foreground(theme.Primary).Bold(true)

		var selectedBody []string
		// title
		selectedBody = append(selectedBody, selectedTree.Render("")+selectedTitle.Render(i.TitleText))
		// containers
		for idx, container := range i.Containers {
			rendered := selectedText.Render(container)
			if idx%2 == 1 {
				rendered = selectedItalic.Render(container)
			}
			selectedBody = append(selectedBody, selectedTree.Render("├ ")+rendered)
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
		selectedRenderedStatus := selectedTree.Render("└ ") + greenBullet + " " + selectedItalic.Render(statusText)
		selectedLines := fitLinesToHeight(selectedBody, selectedRenderedStatus, d.Height(), width)
		block = lipgloss.NewStyle().Bold(true).Render(strings.Join(selectedLines, "\n"))
	}

	_, _ = fmt.Fprint(w, block)
}

// fitLinesToHeight ensures the rendered block has exactly h lines.
// It takes body lines (title + containers) and a final status line, and
// returns a slice of exactly h lines with the status as the last line.
func fitLinesToHeight(body []string, statusLine string, h int, width int) []string {
	if h <= 1 {
		// edge case: at least render status
		return []string{clampLine(statusLine, width)}
	}
	result := make([]string, 0, h)
	// number of body lines we can show
	show := h - 1
	if len(body) >= show {
		result = append(result, body[:show]...)
	} else {
		result = append(result, body...)
		// pad blanks to align next items cleanly
		for len(result) < show {
			result = append(result, "")
		}
	}
	// always put status as the last line
	result = append(result, statusLine)
	for idx, line := range result {
		result[idx] = clampLine(line, width)
	}
	return result
}

func clampLine(line string, width int) string {
	if width <= 0 {
		return line
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(line)
}
