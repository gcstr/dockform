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
	return "│ Dockform will apply the changes listed above.\n" +
		"│ Type " + styleAdd.Bold(true).Italic(true).Render("yes") + " to confirm.\n" +
		"│\n" +
		"│ Answer: " + m.ti.View()
}

// ConfirmIdentifierTTY runs a Bubble Tea prompt that asks the user to type the
// provided identifier to confirm a destroy operation. It renders only when
// attached to a TTY via tea.WithInput/WithOutput provided by the caller.
// It returns whether the user confirmed and the raw value that was entered.
func ConfirmIdentifierTTY(in io.Reader, out io.Writer, identifier string) (bool, string, error) {
	// If either side isn't a TTY, fall back to simple line read.
	if fin, ok := in.(*os.File); !ok || !isatty.IsTerminal(fin.Fd()) {
		rd := bufio.NewReader(in)
		s, _ := rd.ReadString('\n')
		v := strings.TrimSpace(s)
		return v == identifier, v, nil
	}
	if fout, ok := out.(*os.File); !ok || !isatty.IsTerminal(fout.Fd()) {
		rd := bufio.NewReader(in)
		s, _ := rd.ReadString('\n')
		v := strings.TrimSpace(s)
		return v == identifier, v, nil
	}

	m := newConfirmIdentifierModel(identifier)
	opts := []tea.ProgramOption{tea.WithInput(in), tea.WithOutput(out)}
	p := tea.NewProgram(m, opts...)
	finalModel, err := p.Run()
	if err != nil {
		return false, "", err
	}
	cm := finalModel.(confirmIdentifierModel)
	return cm.confirmed, cm.value, nil
}

// confirmIdentifierModel renders a destroy confirmation prompt that asks the
// user to type the exact identifier to proceed.

type confirmIdentifierModel struct {
	ti         textinput.Model
	identifier string
	confirmed  bool
	value      string
}

func newConfirmIdentifierModel(identifier string) confirmIdentifierModel {
	ti := textinput.New()
	ti.Placeholder = identifier
	ti.Cursor.Style = styleInfoPrefix // reuse blue style for cursor
	ti.Focus()
	return confirmIdentifierModel{ti: ti, identifier: identifier}
}

func (m confirmIdentifierModel) Init() tea.Cmd { return textinput.Blink }

func (m confirmIdentifierModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type { //nolint:exhaustive
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			m.value = strings.TrimSpace(m.ti.Value())
			m.confirmed = m.value == m.identifier
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

func (m confirmIdentifierModel) View() string {
	// Summary with left border, then instruction with green-styled identifier, then Answer line
	return "│ This will destroy ALL managed resources with identifier '" + m.identifier + "'.\n" +
		"│ This operation is IRREVERSIBLE.\n" +
		"│\n" +
		"│ Type the identifier name '" + styleAdd.Bold(true).Italic(true).Render(m.identifier) + "' to confirm.\n" +
		"│\n" +
		"│ Answer: " + m.ti.View()
}
