package components

import (
	"github.com/charmbracelet/bubbles/v2/viewport"
	tea "github.com/charmbracelet/bubbletea/v2"
)

// LogsPager is a thin wrapper around a viewport pager that we can embed
// inside the dashboard center column.
type LogsPager struct {
	vp    viewport.Model
	ready bool
}

func NewLogsPager() LogsPager {
	return LogsPager{vp: viewport.New()}
}

func (p *LogsPager) SetSize(width, height int) {
	p.vp.SetWidth(width)
	p.vp.SetHeight(height)
	p.ready = true
}

func (p *LogsPager) SetContent(content string) {
	p.vp.SetContent(content)
	// Always keep the viewport scrolled to the bottom when content changes
	p.vp.GotoBottom()
}

func (p LogsPager) Update(msg tea.Msg) (LogsPager, tea.Cmd) {
	var cmd tea.Cmd
	p.vp, cmd = p.vp.Update(msg)
	return p, cmd
}

func (p LogsPager) View() string {
	if !p.ready {
		return ""
	}
	return p.vp.View()
}
