package ui

import (
	"fmt"
	"io"
	"os"
	"sync"

	bubbleprogress "github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// Progress renders a simple in-place progress bar to a TTY. When the output is
// not a terminal, it is disabled and all methods become no-ops.
type Progress struct {
	out     io.Writer
	label   string
	style   lipgloss.Style
	width   int
	enabled bool

	mu      sync.Mutex
	total   int
	current int
	model   bubbleprogress.Model
	action  string
}

// NewProgress creates a new progress bar writer targeting out with a label.
// The bar only renders if out is a TTY.
func NewProgress(out io.Writer, label string) *Progress {
	enabled := false
	if f, ok := out.(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		enabled = true
	}
	p := &Progress{
		out:     out,
		label:   label,
		style:   lipgloss.NewStyle().Foreground(lipgloss.Color("69")),
		width:   65,
		enabled: enabled,
	}
	m := bubbleprogress.New(bubbleprogress.WithScaledGradient("#3478F6", "#53B6F9"))
	m.Width = p.width
	p.model = m
	return p
}

// Start sets the total and renders the initial bar.
func (p *Progress) Start(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total = total
	p.current = 0
	p.render()
}

// Increment adds one to the current progress and re-renders.
func (p *Progress) Increment() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.total <= 0 {
		return
	}
	if p.current < p.total {
		p.current++
	}
	p.render()
}

// AdjustTotal changes the total by delta (can be negative) and re-renders.
func (p *Progress) AdjustTotal(delta int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total += delta
	if p.total < p.current {
		p.total = p.current
	}
	p.render()
}

// Stop clears the action line and moves cursor back to the spinner/bar line.
func (p *Progress) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled || p.out == nil {
		return
	}
	// Move to next line, clear it, then move back up
	_, _ = fmt.Fprint(p.out, "\n\x1b[2K\x1b[1A")
}

// SetAction sets the action text shown under the progress bar and re-renders.
func (p *Progress) SetAction(text string) {
	p.mu.Lock()
	p.action = text
	p.mu.Unlock()
	p.render()
}

func (p *Progress) render() {
	if !p.enabled || p.out == nil || p.total <= 0 {
		return
	}
	frac := float64(p.current) / float64(p.total)
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	view := p.model.ViewAs(frac)
	// Draw bar on spinner line: skip past " <spinner> " (3 columns) and render
	_, _ = fmt.Fprintf(p.out, "\r\x1b[3C%s %d/%d", view, p.current, p.total)
	// Draw action on next line and return cursor to spinner line
	_, _ = fmt.Fprintf(p.out, "\n\x1b[2K%s\x1b[1A", "   "+p.action)
}
