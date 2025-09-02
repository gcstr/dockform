package ui

import (
	"bufio"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

// ConfirmYesTTY runs a Bubble Tea prompt that asks the user to type "yes" to
// confirm. It renders only when attached to a TTY via tea.WithInput/WithOutput
// provided by the caller. It returns whether the user confirmed and the raw
// value that was entered.
func ConfirmYesTTY(in io.Reader, out io.Writer) (bool, string, error) {
	// If either side isn't a TTY, fall back to simple line read.
	if fin, ok := in.(*os.File); !ok || !isatty.IsTerminal(fin.Fd()) {
		rd := bufio.NewReader(in)
		s, _ := rd.ReadString('\n')
		v := strings.TrimSpace(s)
		return v == "yes", v, nil
	}
	if fout, ok := out.(*os.File); !ok || !isatty.IsTerminal(fout.Fd()) {
		rd := bufio.NewReader(in)
		s, _ := rd.ReadString('\n')
		v := strings.TrimSpace(s)
		return v == "yes", v, nil
	}

	m := newConfirmModel()
	opts := []tea.ProgramOption{tea.WithInput(in), tea.WithOutput(out)}
	p := tea.NewProgram(m, opts...)
	finalModel, err := p.Run()
	if err != nil {
		return false, "", err
	}
	cm := finalModel.(confirmModel)
	return cm.confirmed, cm.value, nil
}

type confirmModel struct {
	ti        textinput.Model
	confirmed bool
	value     string
}

func newConfirmModel() confirmModel {
	ti := textinput.New()
	ti.Placeholder = "yes"
	ti.Cursor.Style = styleInfoPrefix // reuse blue style for cursor
	ti.Focus()
	return confirmModel{ti: ti}
}

func (m confirmModel) Init() tea.Cmd { return textinput.Blink }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type { //nolint:exhaustive
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			m.value = strings.TrimSpace(m.ti.Value())
			m.confirmed = m.value == "yes"
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

func (m confirmModel) View() string {
	return " Dockform will apply the changes listed above.\n" +
		" Type " + styleAdd.Bold(true).Italic(true).Render("yes") + " to confirm.\n\n" +
		" Answer: " + m.ti.View()
}
