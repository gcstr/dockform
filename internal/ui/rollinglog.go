package ui

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/gcstr/dockform/internal/logger"
)

// RunWithRollingLog runs fn while showing a 5-line rolling block fed by the existing logger.
// The adapter attaches/detaches to the logger automatically when stdout is a TTY.
// On completion, the rolling block is replaced by the final report in the same view and the
// area below is cleared. Returns the final report and error.
func RunWithRollingLog(ctx context.Context, fn func(ctx context.Context) (string, error)) (string, error) {
	// Non-TTY path: bypass UI entirely.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return fn(ctx)
	}

	// Signal to other UI helpers to suppress printing while rolling log is active
	_ = os.Setenv("DOCKFORM_TUI_ACTIVE", "1")
	// Best-effort: force color for libraries that consult these env vars
	prevCliColorForce := os.Getenv("CLICOLOR_FORCE")
	prevForceColor := os.Getenv("FORCE_COLOR")
	_ = os.Setenv("CLICOLOR_FORCE", "1")
	_ = os.Setenv("FORCE_COLOR", "1")
	defer func() {
		os.Unsetenv("DOCKFORM_TUI_ACTIVE")
		if prevCliColorForce == "" {
			os.Unsetenv("CLICOLOR_FORCE")
		} else {
			_ = os.Setenv("CLICOLOR_FORCE", prevCliColorForce)
		}
		if prevForceColor == "" {
			os.Unsetenv("FORCE_COLOR")
		} else {
			_ = os.Setenv("FORCE_COLOR", prevForceColor)
		}
	}()

	// Build Bubble Tea program (no alt screen)
	m := model{state: stateRunning, width: 80}
	p := tea.NewProgram(m, tea.WithOutput(os.Stdout))

	// Create UILogWriter that forwards each fully formatted line to the UI.
	uis := &UILogWriter{send: func(s string) { p.Send(appendLog{line: s}) }}

	// Construct a logger sink that formats via the existing path and writes to UILogWriter.
	uiLogger, uiCloser, err := logger.New(logger.Options{Out: uis, Level: "info", Format: "pretty"})
	if err != nil {
		return "", err
	}
	if uiCloser != nil {
		defer uiCloser.Close()
	}

	// Fan out: base logger + UI sink
	base := logger.FromContext(ctx)
	fanout := logger.Fanout(base, uiLogger)
	ctx = logger.WithContext(ctx, fanout)

	// Run Bubble Tea in background
	var runErr error
	doneCh := make(chan struct{})
	go func() {
		_, runErr = p.Run()
		close(doneCh)
	}()

	// Execute the job while UI is running
	finalReport, err := fn(ctx)
	// Notify UI to render final report and exit
	p.Send(done{report: finalReport})
	<-doneCh

	if runErr != nil && err == nil {
		// Prefer job error if any; otherwise return UI error
		err = runErr
	}
	return finalReport, err
}

// UILogWriter buffers bytes and sends complete lines to the UI.
type UILogWriter struct {
	send func(string)
	mu   sync.Mutex
	buf  bytes.Buffer
}

// Fd reports a real terminal file descriptor so color libraries treat this
// writer as a TTY. We reuse stdout's FD because the UI renders to stdout.
func (w *UILogWriter) Fd() uintptr { return os.Stdout.Fd() }

func (w *UILogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, _ := w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// put partial back
			if len(line) > 0 {
				// reset and keep partial
				var nb bytes.Buffer
				nb.WriteString(line)
				w.buf = nb
			}
			break
		}
		line = strings.TrimRight(line, "\r\n")
		if line != "" {
			w.send(line)
		}
	}
	return n, nil
}

// Bubble Tea model -----------------------------------------------------------

type state int

const (
	stateRunning state = iota
	stateFinal
)

type appendLog struct{ line string }
type done struct{ report string }

type model struct {
	state       state
	width       int
	logLines    []string // newest last, max 5
	finalReport string
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case appendLog:
		m.logLines = append(m.logLines, msg.line)
		if len(m.logLines) > 5 {
			m.logLines = m.logLines[len(m.logLines)-5:]
		}
	case done:
		m.finalReport = msg.report
		m.state = stateFinal
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	// neutral gray style for rolling log lines
	styleLog := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	switch m.state {
	case stateRunning:
		for _, l := range m.logLines {
			b.WriteString(styleLog.Render("│ "))
			// Preserve original ANSI colors in the log content
			b.WriteString(truncOneRowANSI(l, m.width))
			b.WriteByte('\n')
		}
		// spacer line below the rolling block
		b.WriteByte('\n')
	case stateFinal:
		if m.finalReport != "" {
			b.WriteString(m.finalReport)
			b.WriteByte('\n')
		}
		// Clear any lines below
		b.WriteString("\x1b[0J")
	}
	return b.String()
}

// truncOneRowANSI ensures the content fits exactly one physical row, accounting
// for the left border width (2). ANSI-aware width via lipgloss.Width.
func truncOneRowANSI(s string, width int) string {
	if width <= 2 {
		return ""
	}
	limit := width - 2
	// If already fits, return as-is
	if lipgloss.Width(s) <= limit {
		return s
	}
	// Truncate by runes conservatively until width fits.
	// This is simple but effective; lipgloss.Width handles ANSI sequences.
	r := []rune(s)
	// Binary search could be used; linear is fine for short lines.
	for i := range r {
		candidate := string(r[:i])
		if lipgloss.Width(candidate) > limit {
			if i == 0 {
				return ""
			}
			return string(r[:i-1])
		}
	}
	// Fallback
	return string(r)
}
