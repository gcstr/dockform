package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/gcstr/dockform/internal/logger"
)

// activeProgram tracks the Bubble Tea program currently owning stdout while
// RunWithRollingLog is active. Spinner.SetLabel reads this to forward label
// updates into the rolling log view instead of animating on stdout directly.
var (
	activeMu      sync.RWMutex
	activeProgram *tea.Program
)

func getActiveProgram() *tea.Program {
	activeMu.RLock()
	defer activeMu.RUnlock()
	return activeProgram
}

func setActiveProgram(p *tea.Program) {
	activeMu.Lock()
	activeProgram = p
	activeMu.Unlock()
}

// maxLogLines is the number of rolling log lines displayed in the UI.
const maxLogLines = 5

// RunWithRollingLog runs fn while showing a rolling block of the last 5 log
// lines fed by the existing logger. The adapter attaches/detaches to the logger
// automatically when stdout is a TTY.
//
// On completion, the rolling block is replaced by the final report and the area
// below is cleared. Returns the final report and error.
func RunWithRollingLog(ctx context.Context, fn func(ctx context.Context) (string, error)) (string, error) {
	// Non-TTY path: bypass UI entirely.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return fn(ctx)
	}

	// Signal to other UI helpers (Spinner, StdPrinter) to suppress direct
	// stdout writes while Bubble Tea owns the terminal.
	_ = os.Setenv("DOCKFORM_TUI_ACTIVE", "1")
	defer func() { _ = os.Unsetenv("DOCKFORM_TUI_ACTIVE") }()

	// Create a cancellable context so we can stop the work when Ctrl+C is pressed
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Channel to signal when user presses Ctrl+C in the UI
	cancelCh := make(chan struct{})

	// Initialise model with the real terminal width so truncation is correct
	// from the very first render (before WindowSizeMsg arrives).
	initialWidth, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || initialWidth <= 0 {
		initialWidth = 80
	}

	// Build Bubble Tea program (no alt screen)
	m := model{state: stateRunning, width: initialWidth, cancelCh: cancelCh}
	p := tea.NewProgram(m, tea.WithOutput(os.Stdout))

	// Expose the program so Spinner.SetLabel can forward status updates to
	// the rolling UI instead of fighting for stdout.
	setActiveProgram(p)
	defer setActiveProgram(nil)

	// Wire up display logger: intercepts structured log events and formats
	// lines for the UI via appendLog messages.
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

var statusSpinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("69"))

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

	// sanitizeSingleLine normalizes whitespace/control chars that would either
	// break single-line display or inflate the rendered width past what
	// ansi.StringWidth reports (tabs expand to tab stops on the terminal but
	// measure as 0 cells, which silently overflows our truncation target and
	// causes wrap — that in turn breaks Bubble Tea's cursor-up arithmetic).
	sanitizeSingleLine := func(s string) string {
		return strings.Map(func(r rune) rune {
			switch r {
			case '\n', '\r':
				return '↵'
			case '\t':
				return ' '
			}
			if r < 0x20 { // strip other C0 control chars
				return ' '
			}
			return r
		}, s)
	}

	var sb strings.Builder
	sb.WriteString(ts)
	sb.WriteByte(' ')
	sb.WriteString(lvlStyle.Render(level))
	sb.WriteByte(' ')
	sb.WriteString(sanitizeSingleLine(msg))

	// Walk key=value pairs: skip noise, collapse status+action, render the rest.
	var pendingStatus string
	for i := 0; i+1 < len(all); i += 2 {
		key, ok := all[i].(string)
		if !ok {
			continue
		}
		val := sanitizeSingleLine(fmt.Sprintf("%v", all[i+1]))

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

type appendLog struct{ line string }
type done struct{ report string }
type interrupted struct{}
type statusUpdate struct{ label string }
type statusTick struct{}

// statusSpinnerFrames mirrors ui.Spinner so the TUI and the standalone
// spinner use the same animation.
var statusSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const statusTickDelay = 100 * time.Millisecond

func statusTickCmd() tea.Cmd {
	return tea.Tick(statusTickDelay, func(time.Time) tea.Msg { return statusTick{} })
}

type model struct {
	state       state
	width       int
	logLines    []string // newest last, max maxLogLines
	finalReport string
	cancelCh    chan struct{} // Signal channel for Ctrl+C
	statusLabel string        // current progress label (e.g., "Applying -> creating volume foo")
	statusFrame int           // spinner animation frame
}

func (m model) Init() tea.Cmd { return statusTickCmd() }

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
		m.logLines = append(m.logLines, msg.line)
		if len(m.logLines) > maxLogLines {
			m.logLines = m.logLines[len(m.logLines)-maxLogLines:]
		}
	case statusUpdate:
		m.statusLabel = msg.label
	case statusTick:
		if m.state == stateRunning {
			m.statusFrame++
			return m, statusTickCmd()
		}
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

// borderPrefix is the left-margin gutter rendered before each log line.
// Matches the "│ " used by the Identifier/Contexts header so the rolling
// block visually continues that gutter.
const borderPrefix = "│ "

func (m model) View() string {
	var b strings.Builder
	switch m.state {
	case stateRunning:
		// Status line: animated frame + current label (e.g., "Applying -> creating volume foo").
		// DisplayDaemonInfo prints a trailing blank line, so we don't add our own leading spacer.
		// Sits above the rolling log so the current phase is always visible.
		if m.statusLabel != "" {
			frame := statusSpinnerFrames[m.statusFrame%len(statusSpinnerFrames)]
			statusLine := borderPrefix + statusSpinnerStyle.Render(frame) + " " + m.statusLabel
			if m.width > 1 {
				statusLine = ansi.Truncate(statusLine, m.width-1, "")
			}
			b.WriteString(statusLine)
			b.WriteByte('\n')
			b.WriteByte('\n')
		}
		for _, l := range m.logLines {
			// Build the complete line (border + content) first, then truncate
			// the WHOLE thing to m.width-1. This guarantees the line never
			// reaches the terminal's last column, which would trigger "pending
			// autowrap" and break Bubble Tea's cursor-up line accounting.
			//
			// We subtract 1 from m.width as a safety margin: some terminal
			// emulators move to the next row when a character lands in the
			// last column, even without an explicit newline.
			fullLine := borderPrefix + l
			if m.width > 1 {
				fullLine = ansi.Truncate(fullLine, m.width-1, "")
			}
			b.WriteString(fullLine)
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
// for the left border width (2). Uses ansi.Truncate for proper ANSI-sequence
// handling: escape codes are never split, and any open sequences are closed at
// the truncation point so subsequent lines are not left with stale style state.
func truncOneRowANSI(s string, width int) string {
	if width <= 2 {
		return ""
	}
	return ansi.Truncate(s, width-2, "")
}
