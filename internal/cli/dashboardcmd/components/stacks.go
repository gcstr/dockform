package components

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
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

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#DEDEDE")).Bold(true)
	subStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#A6A6A6"))
	subItalicStyle := subStyle.Italic(true)
	greenBullet := lipgloss.NewStyle().Foreground(lipgloss.Color("#00B341")).Render("●")

	// Reduced indentation
	lines = append(lines, " "+titleStyle.Render(i.TitleText))

	for idx, container := range i.Containers {
		rendered := subStyle.Render(container)
		if idx == 1 {
			rendered = subItalicStyle.Render(container)
		}
		lines = append(lines, " ├ "+rendered)
	}

	statusText := i.Status
	if strings.HasPrefix(statusText, "●") || strings.HasPrefix(statusText, "○") {
		remainder := strings.TrimSpace(strings.TrimLeft(statusText, "●○ "))
		statusText = greenBullet + " " + subItalicStyle.Render(remainder)
	} else {
		statusText = greenBullet + " " + subItalicStyle.Render(statusText)
	}
	lines = append(lines, " └ "+statusText)

	block := strings.Join(lines, "\n")
	if index == m.Index() {
		block = lipgloss.NewStyle().Bold(true).Render(block)
	}

	_, _ = fmt.Fprint(w, block)
}
