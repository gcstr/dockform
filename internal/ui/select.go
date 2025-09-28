package ui

import (
	"bufio"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// SelectOneTTY renders a simple Bubble Tea picker to choose one option.
// It only renders when both in and out are TTY; otherwise returns an error-like false ok.
// Returns the selected index, whether a selection was made, and an error if Bubble Tea fails.
func SelectOneTTY(in io.Reader, out io.Writer, title string, options []string) (int, bool, error) {
	if fin, ok := in.(*os.File); !ok || !isatty.IsTerminal(fin.Fd()) {
		return -1, false, nil
	}
	if fout, ok := out.(*os.File); !ok || !isatty.IsTerminal(fout.Fd()) {
		return -1, false, nil
	}

	m := selectModel{title: title, options: options}
	opts := []tea.ProgramOption{tea.WithInput(in), tea.WithOutput(out)}
	p := tea.NewProgram(m, opts...)
	finalModel, err := p.Run()
	if err != nil {
		return -1, false, err
	}
	sm := finalModel.(selectModel)
	return sm.choice, sm.confirmed, nil
}

type selectModel struct {
	title     string
	options   []string
	cursor    int
	choice    int
	confirmed bool
}

func (m selectModel) Init() tea.Cmd { return nil }

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() { //nolint:exhaustive
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.options) == 0 {
				return m, tea.Quit
			}
			m.choice = m.cursor
			m.confirmed = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectModel) View() string {
	var b strings.Builder
	// Add blank line above title
	b.WriteString("\n")
	// Title
	// b.WriteString(" ")
	b.WriteString(styleSectionTitle.Render(m.title))
	b.WriteString("\n")
	// Options
	for i, opt := range m.options {
		if i == m.cursor {
			// Highlight the entire line (prefix + text) in purple/magenta like the example
			purpleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#C084FC")) // Purple-400
			wholeLine := "[x] " + opt
			b.WriteString(purpleStyle.Render(wholeLine))
		} else {
			b.WriteString("[ ] " + opt)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ReadLineNonTTY reads one line from the reader, trimming trailing newline. Useful for non-tty fallbacks.
func ReadLineNonTTY(in io.Reader) (string, error) {
	rd := bufio.NewReader(in)
	s, err := rd.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}
