package ui

import (
	"bytes"
	"io"
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// RenderYAMLInPagerTTY displays YAML content in a full-screen viewport pager when
// attached to a TTY. When not connected to a TTY, it falls back to plain output
// identical to standard printing (ensuring a trailing newline).
func RenderYAMLInPagerTTY(in io.Reader, out io.Writer, yamlContent string, title string) error {
	// Fallback to plain output if not a terminal on either side.
	finTTY := false
	if fin, ok := in.(*os.File); ok && isatty.IsTerminal(fin.Fd()) {
		finTTY = true
	}
	foutTTY := false
	if fout, ok := out.(*os.File); ok && isatty.IsTerminal(fout.Fd()) {
		foutTTY = true
	}
	if !finTTY || !foutTTY {
		// Non-TTY: print raw YAML without ANSI and ensure trailing newline.
		if _, err := io.WriteString(out, yamlContent); err != nil {
			return err
		}
		if !strings.HasSuffix(yamlContent, "\n") {
			if _, err := io.WriteString(out, "\n"); err != nil {
				return err
			}
		}
		return nil
	}

	// TTY: syntax highlight with Chroma and render in a full-screen viewport.
	highlighted := colorizeYAML(yamlContent)

	m := newPagerModel(title, highlighted)
	opts := []tea.ProgramOption{tea.WithInput(in), tea.WithOutput(out), tea.WithAltScreen()}
	p := tea.NewProgram(m, opts...)
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

func colorizeYAML(src string) string {
	var b bytes.Buffer
	// terminal256 formatter for broad compatibility; fallback to plain on error.
	if err := quick.Highlight(&b, src, "yaml", "terminal256", "base16-snazzy"); err != nil {
		return src
	}
	return b.String()
}

type pagerModel struct {
	title    string
	content  string
	viewport viewport.Model
	ready    bool
}

func newPagerModel(title, content string) pagerModel {
	vp := viewport.New(0, 0)
	vp.SetContent(content)
	return pagerModel{title: title, content: content, viewport: vp}
}

func (m pagerModel) Init() tea.Cmd {
	// No-op; we enter the alt screen via Program option.
	return nil
}

func (m pagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type { //nolint:exhaustive
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		}
		switch msg.String() {
		case "q":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		// Set width first, then measure header, then compute viewport height.
		m.viewport.Width = msg.Width
		headerHeight := lipgloss.Height(m.headerView())
		// Leave at least one line for content
		h := msg.Height - headerHeight
		if h < 1 {
			h = 1
		}
		m.viewport.Height = h
		// Reflow content to the new width.
		m.viewport.SetContent(m.content)
		m.ready = true
		return m, nil
	}
	return m, nil
}

func (m pagerModel) View() string {
	if !m.ready {
		return ""
	}
	return lipgloss.JoinVertical(lipgloss.Top, m.headerView(), m.viewport.View())
}

func (m pagerModel) headerView() string {
	// Simple header bar with title and hint.
	left := styleSectionTitle.Render(m.title)
	right := lipgloss.NewStyle().Faint(true).Render("q to quit")

	gap := "  "
	header := left + gap + right
	// Add a subtle divider below the header
	divider := lipgloss.NewStyle().Faint(true).Render(strings.Repeat("─", max(0, m.viewport.Width)))
	return header + "\n" + divider
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
