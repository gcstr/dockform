package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/gcstr/dockform/internal/logger"
)

// RunWithRollingLog runs fn while streaming structured log lines to the terminal
// via Bubble Tea's Println mechanism. Each log line prints above the active view
// and scrolls up naturally — no cursor arithmetic is required, so the display
// stays stable even when lines are longer than the terminal width.
//
// On completion the view is replaced by finalReport. Returns the report and error.
func RunWithRollingLog(ctx context.Context, fn func(ctx context.Context) (string, error)) (string, error) {
	// Non-TTY path: bypass UI entirely.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return fn(ctx)
	}

	// Signal to other UI helpers to suppress printing while rolling log is active
	_ = os.Setenv("DOCKFORM_TUI_ACTIVE", "1")
	defer func() { _ = os.Unsetenv("DOCKFORM_TUI_ACTIVE") }()

	// Create a cancellable context so we can stop the work when Ctrl+C is pressed
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Channel to signal when user presses Ctrl+C in the UI
	cancelCh := make(chan struct{})

	// Build Bubble Tea program (no alt screen). The view is kept to a single
	// blank line so the cursor-up arithmetic is trivially correct.
	m := model{state: stateRunning, cancelCh: cancelCh}
	p := tea.NewProgram(m, tea.WithOutput(os.Stdout))

	// Wire up display logger: formats structured log events into display lines
	// and sends them as appendLog messages. The model converts each appendLog
	// into a tea.Println Cmd, which streams lines above the view via Bubble Tea's
	// queued-messages mechanism — no cursor arithmetic required.
	displayLog := newDisplayLogger(func(line string) { p.Send(appendLog{line: line}) })

	// Fan out: base logger (file/stderr) + display logger (rolling UI)
	base := logger.FromContext(ctx)
	ctx = logger.WithContext(ctx, logger.Fanout(base, displayLog))

	// Run Bubble Tea in background
	var runErr error
	doneCh := make(chan struct{})
	go func() {
		_, runErr = p.Run()
		close(doneCh)
	}()

	// Monitor for UI cancellation (Ctrl+C in Bubble Tea) and parent context cancellation
	go func() {
		select {
		case <-cancelCh:
			// User pressed Ctrl+C in the UI
			cancel()
			p.Send(interrupted{})
		case <-ctx.Done():
			// Parent context was cancelled (e.g., signal from OS)
			p.Send(interrupted{})
		}
	}()

	// Execute the job while UI is running
	finalReport, err := fn(ctx)

	// Check if context was cancelled - if so, don't send the report
	if ctx.Err() != nil {
		// Context was cancelled, wait for UI to finish showing interrupt message
		<-doneCh
		return "", ctx.Err()
	}

	// Notify UI to render final report and exit
	p.Send(done{report: finalReport})
	<-doneCh

	if runErr != nil && err == nil {
		// Prefer job error if any; otherwise return UI error
		err = runErr
	}
	return finalReport, err
}

// displayLogger implements logger.Logger and formats log lines directly from
// structured data for the rolling log UI — no text parsing required.
//
// Fields stripped from display: run_id, command (CLI noise).
// Fields collapsed: status + action → "action(status)".
// Everything else is rendered as dim "key=value" pairs.
type displayLogger struct {
	send func(string)
	base []any // persistent key=value pairs from With()
}

func newDisplayLogger(send func(string)) *displayLogger {
	return &displayLogger{send: send}
}

// Debug is intentionally a no-op: debug lines are too verbose for the rolling UI.
func (d *displayLogger) Debug(string, ...any) {}

func (d *displayLogger) Info(msg string, kvs ...any)  { d.emit("INFO", msg, kvs) }
func (d *displayLogger) Warn(msg string, kvs ...any)  { d.emit("WARN", msg, kvs) }
func (d *displayLogger) Error(msg string, kvs ...any) { d.emit("ERROR", msg, kvs) }

func (d *displayLogger) With(kvs ...any) logger.Logger {
	merged := make([]any, len(d.base)+len(kvs))
	copy(merged, d.base)
	copy(merged[len(d.base):], kvs)
	return &displayLogger{send: d.send, base: merged}
}

// displayNoiseKeys are key=value fields always omitted from the UI.
var displayNoiseKeys = map[string]bool{
	"run_id":  true,
	"command": true,
}

var (
	displayStyleKey    = lipgloss.NewStyle().Faint(true)
	displayLevelStyles = map[string]lipgloss.Style{
		"INFO":  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")), // bright cyan
		"WARN":  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")), // bright yellow
		"ERROR": lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9")),  // bright red
	}
)

func (d *displayLogger) emit(level, msg string, callKVs []any) {
	// Merge persistent base fields with call-site fields.
	all := make([]any, len(d.base)+len(callKVs))
	copy(all, d.base)
	copy(all[len(d.base):], callKVs)

	ts := time.Now().Format("15:04:05")

	lvlStyle, ok := displayLevelStyles[level]
	if !ok {
		lvlStyle = lipgloss.NewStyle()
	}

	var sb strings.Builder
	sb.WriteString(ts)
	sb.WriteByte(' ')
	sb.WriteString(lvlStyle.Render(level))
	sb.WriteByte(' ')
	sb.WriteString(msg)

	// Walk key=value pairs: skip noise, collapse status+action, render the rest.
	var pendingStatus string
	for i := 0; i+1 < len(all); i += 2 {
		key, ok := all[i].(string)
		if !ok {
			continue
		}
		val := fmt.Sprintf("%v", all[i+1])
		// Sanitize control chars that would break single-line display.
		val = strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' {
				return '↵'
			}
			return r
		}, val)

		if displayNoiseKeys[key] {
			continue
		}
		if key == "status" {
			pendingStatus = val
			continue
		}
		if key == "action" {
			if pendingStatus != "" {
				sb.WriteByte(' ')
				sb.WriteString(val + "(" + pendingStatus + ")")
				pendingStatus = ""
			}
			continue
		}

		// Quote values that contain spaces or tabs.
		if strings.ContainsAny(val, " \t") {
			val = `"` + val + `"`
		}
		sb.WriteByte(' ')
		sb.WriteString(displayStyleKey.Render(key + "="))
		sb.WriteString(val)
	}

	d.send(sb.String())
}

// Bubble Tea model -----------------------------------------------------------

type state int

const (
	stateRunning state = iota
	stateFinal
)

// appendLog carries a single formatted log line from the displayLogger.
// The model converts it to a tea.Println Cmd so it is printed above the
// view via Bubble Tea's queued-messages mechanism rather than in View().
type appendLog struct{ line string }
type done struct{ report string }
type interrupted struct{}

type model struct {
	state       state
	width       int
	finalReport string
	cancelCh    chan struct{} // Signal channel for Ctrl+C
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle Ctrl+C keyboard interrupt
		if msg.Type == tea.KeyCtrlC {
			// Signal cancellation to stop the background work
			if m.cancelCh != nil {
				select {
				case m.cancelCh <- struct{}{}:
				default:
					// Already signaled, ignore
				}
			}
			// Don't quit immediately; wait for interrupted message after context cancels
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case appendLog:
		// Convert the log line into a tea.Println Cmd. Bubble Tea runs the Cmd
		// in a goroutine and routes the resulting printLineMessage through its
		// safe send path (context-guarded), so this never deadlocks even if the
		// program exits before the Cmd executes.
		return m, tea.Println(msg.line)
	case done:
		m.finalReport = msg.report
		m.state = stateFinal
		return m, tea.Quit
	case interrupted:
		// Handle interrupt signal by quitting immediately
		m.finalReport = "\n│ Interrupted by user (Ctrl+C)\n"
		m.state = stateFinal
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() string {
	switch m.state {
	case stateRunning:
		// Keep the view to a single blank line. Log lines are streamed via
		// p.Println (queued messages) which prints above this view without
		// any cursor-up arithmetic — immune to physical line wrapping.
		return "\n"
	case stateFinal:
		var b strings.Builder
		if m.finalReport != "" {
			b.WriteString(m.finalReport)
			b.WriteByte('\n')
		}
		// Clear any lines below
		b.WriteString("\x1b[0J")
		return b.String()
	}
	return "\n"
}

// truncOneRowANSI ensures the content fits exactly one physical row, accounting
// for the left border width (2). Uses ansi.Truncate for proper ANSI-sequence
// handling: escape codes are never split, and any open sequences are closed at
// the truncation point so subsequent lines are not left with stale style state.
func truncOneRowANSI(s string, width int) string {
	if width <= 2 {
		return ""
	}
	return ansi.Truncate(s, width-2, "")
}
