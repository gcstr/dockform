package ui

import (
	"bytes"
	"fmt"
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
	if err := quick.Highlight(&b, src, "yaml", "terminal256", "vulcan"); err != nil {
		return src
	}
	return b.String()
}

type pagerModel struct {
	title           string
	content         string
	originalContent string
	viewport        viewport.Model
	ready           bool
}

func newPagerModel(title, content string) pagerModel {
	vp := viewport.New(0, 0)
	// Add line numbers and format content
	formattedContent := formatContentWithLineNumbers(content)
	vp.SetContent(formattedContent)
	return pagerModel{title: title, content: formattedContent, originalContent: content, viewport: vp}
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
		// Set width first, then measure header and footer, then compute viewport height.
		m.viewport.Width = msg.Width
		headerHeight := lipgloss.Height(m.headerView())
		footerHeight := lipgloss.Height(m.footerView())
		// Leave at least one line for content
		h := msg.Height - headerHeight - footerHeight
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
	return lipgloss.JoinVertical(lipgloss.Top, m.headerView(), m.viewport.View(), m.footerView())
}

func (m pagerModel) headerView() string {
	if m.viewport.Width < 10 {
		return ""
	}

	// Calculate line number width based on original content (before formatting)
	originalLines := strings.Count(m.originalContent, "\n") + 1
	lineNumWidth := len(fmt.Sprintf("%d", originalLines))
	if lineNumWidth < 4 {
		lineNumWidth = 4 // Minimum width for 4 digits
	}

	// Style the lines
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Top separator line with junction
	leftPartTop := strings.Repeat("─", lineNumWidth+1)
	junctionTop := "┬"
	rightPartTop := strings.Repeat("─", m.viewport.Width-lineNumWidth-2)
	topLine := leftPartTop + junctionTop + rightPartTop

	// File info line with proper alignment to match line numbers
	padding := strings.Repeat(" ", lineNumWidth)
	separator := separatorStyle.Render(" │ ")
	fileInfo := padding + separator + "File: " + m.title

	// Middle separator with junction at the right position
	leftPart := strings.Repeat("─", lineNumWidth+1)
	junction := "┼"
	rightPart := strings.Repeat("─", m.viewport.Width-lineNumWidth-1)
	middleLine := leftPart + junction + rightPart

	return separatorStyle.Render(topLine) + "\n" +
		fileInfo + "\n" +
		separatorStyle.Render(middleLine)
}

func (m pagerModel) footerView() string {
	if m.viewport.Width < 10 {
		return ""
	}

	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	bottomLine := strings.Repeat("─", m.viewport.Width)
	return separatorStyle.Render(bottomLine)
}

func formatContentWithLineNumbers(content string) string {
	// Calculate width needed for line numbers based on original content
	originalLines := strings.Count(content, "\n") + 1
	lineNumWidth := len(fmt.Sprintf("%d", originalLines))
	if lineNumWidth < 4 {
		lineNumWidth = 4 // Minimum width for 4 digits
	}

	// First apply syntax highlighting
	highlighted := colorizeYAML(content)

	// Split into lines and add line numbers
	lines := strings.Split(highlighted, "\n")
	var numberedLines []string

	lineNumStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	for i, line := range lines {
		lineNum := fmt.Sprintf("%*d", lineNumWidth, i+1)
		styledLineNum := lineNumStyle.Render(lineNum)
		separator := separatorStyle.Render(" │ ")
		numberedLine := styledLineNum + separator + line
		numberedLines = append(numberedLines, numberedLine)
	}

	return strings.Join(numberedLines, "\n")
}
