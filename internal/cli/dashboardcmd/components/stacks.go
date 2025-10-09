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
	TitleText     string
	Service       string
	ContainerName string
	Containers    []string
	Status        string
	FilterText    string
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
	mutedBullet := lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Render("●")
	yellowBullet := lipgloss.NewStyle().Foreground(theme.Warning).Render("●")
	redBullet := lipgloss.NewStyle().Foreground(theme.Error).Render("●")

	bodyLines = append(bodyLines, treeStyle.Render("")+titleStyle.Render(i.TitleText))

	for idx, container := range i.Containers {
		// For the first content line, show only the container name if available; else show the service
		if idx == 0 {
			display := container
			if strings.TrimSpace(i.ContainerName) != "" {
				display = i.ContainerName
			} else if strings.TrimSpace(i.Service) != "" {
				display = i.Service
			}
			rendered := textStyle.Render(display)
			bodyLines = append(bodyLines, treeStyle.Render("├ ")+rendered)
			continue
		}
		rendered := textStyle.Render(container)
		if idx%2 == 1 {
			rendered = textItalicStyle.Render(container)
		}
		bodyLines = append(bodyLines, treeStyle.Render("├ ")+rendered)
	}

	statusText := i.Status
	// Choose bullet color by simple prefix marker if present in Status field
	bullet := mutedBullet
	if strings.HasPrefix(statusText, "[warn]") {
		statusText = strings.TrimSpace(strings.TrimPrefix(statusText, "[warn]"))
		bullet = yellowBullet
	} else if strings.HasPrefix(statusText, "[err]") {
		statusText = strings.TrimSpace(strings.TrimPrefix(statusText, "[err]"))
		bullet = redBullet
	} else if strings.HasPrefix(statusText, "[ok]") {
		statusText = strings.TrimSpace(strings.TrimPrefix(statusText, "[ok]"))
		// green for OK
		bullet = lipgloss.NewStyle().Foreground(theme.Success).Render("●")
	}
	if strings.HasPrefix(statusText, "●") || strings.HasPrefix(statusText, "○") {
		remainder := strings.TrimSpace(strings.TrimLeft(statusText, "●○ "))
		statusText = bullet + " " + textItalicStyle.Render(remainder)
	} else {
		statusText = bullet + " " + textItalicStyle.Render(statusText)
	}
	renderedStatus := treeStyle.Render("└ ") + statusText

	width := m.Width()
	lines := fitLinesToHeight(bodyLines, renderedStatus, d.Height(), width)
	block := strings.Join(lines, "\n")
	if index == m.Index() {
		// Selected state: same content as unselected, different colors
		selectedTree := lipgloss.NewStyle().Foreground(theme.FgBase)
		selectedText := lipgloss.NewStyle().Foreground(theme.FgBase)
		selectedItalic := selectedText.Italic(true)
		selectedTitle := lipgloss.NewStyle().Foreground(theme.Primary).Bold(true)

		var selectedBody []string
		// title
		selectedBody = append(selectedBody, selectedTree.Render("")+selectedTitle.Render(i.TitleText))
		// containers: use the same display logic (ContainerName preferred)
		for idx, container := range i.Containers {
			var display string
			if idx == 0 {
				display = container
				if strings.TrimSpace(i.ContainerName) != "" {
					display = i.ContainerName
				} else if strings.TrimSpace(i.Service) != "" {
					display = i.Service
				}
			} else {
				display = container
			}
			rendered := selectedText.Render(display)
			if idx%2 == 1 {
				rendered = selectedItalic.Render(display)
			}
			selectedBody = append(selectedBody, selectedTree.Render("├ ")+rendered)
		}
		// status: same logic as unselected, but render text with selected styles
		raw := i.Status
		bullet := lipgloss.NewStyle().Foreground(theme.FgHalfMuted).Render("●")
		if strings.HasPrefix(raw, "[warn]") {
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "[warn]"))
			bullet = lipgloss.NewStyle().Foreground(theme.Warning).Render("●")
		} else if strings.HasPrefix(raw, "[err]") {
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "[err]"))
			bullet = lipgloss.NewStyle().Foreground(theme.Error).Render("●")
		} else if strings.HasPrefix(raw, "[ok]") {
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "[ok]"))
			bullet = lipgloss.NewStyle().Foreground(theme.Success).Render("●")
		}
		if strings.HasPrefix(raw, "●") || strings.HasPrefix(raw, "○") {
			raw = strings.TrimSpace(strings.TrimLeft(raw, "●○ "))
		}
		selectedRenderedStatus := selectedTree.Render("└ ") + bullet + " " + selectedItalic.Render(raw)
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
