package ui

import (
    "io"
    "strings"

    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/bubbles/textinput"
)

// ConfirmYesTTY runs a Bubble Tea prompt that asks the user to type "yes" to
// confirm. It renders only when attached to a TTY via tea.WithInput/WithOutput
// provided by the caller. It returns whether the user confirmed and the raw
// value that was entered.
func ConfirmYesTTY(in io.Reader, out io.Writer) (bool, string, error) {
    m := newConfirmModel()
    p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out))
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
    return "Dockform will apply the changes listed above.\n" +
        "Type 'yes' to confirm.\n\n" +
        "Enter a value: " + m.ti.View()
}

