package applyview

import (
	"context"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gcstr/dockform/internal/planner"
	"golang.org/x/term"
)

// reporter turns planner progress events into Bubble Tea messages.
type reporter struct{ send func(tea.Msg) }

// NewReporter returns a ProgressReporter that forwards every event to send as
// a Bubble Tea message. A nil send makes every method a no-op — the shape the
// non-TTY and plain paths need, since there is no program to send to.
func NewReporter(send func(tea.Msg)) planner.ProgressReporter {
	return &reporter{send: send}
}

func (r *reporter) emit(msg tea.Msg) {
	if r == nil || r.send == nil {
		return
	}
	r.send(msg)
}

func (r *reporter) Seed(items []planner.ResourceRef) { r.emit(SeedMsg{Items: items}) }

func (r *reporter) Start(ref planner.ResourceRef, verb string) {
	r.emit(StartMsg{Ref: ref, Verb: verb})
}

func (r *reporter) Detail(ref planner.ResourceRef, text string) {
	r.emit(DetailMsg{Ref: ref, Text: text})
}

func (r *reporter) Finish(ref planner.ResourceRef, result string) {
	r.emit(FinishMsg{Ref: ref, Result: result})
}

func (r *reporter) Fail(ref planner.ResourceRef, err error) {
	r.emit(FailMsg{Ref: ref, Err: err})
}

// Run renders the apply view inline while fn does the work, handing fn the
// reporter to report progress against.
//
// It deliberately mirrors ui.RunWithRollingLog (internal/ui/rollinglog.go)
// rather than inventing its own lifecycle:
//   - no alt screen, so the finished view stays in scrollback;
//   - the same DOCKFORM_TUI_ACTIVE gate that stops ui.Spinner and
//     ui.StdPrinter from writing to stdout underneath the Bubble Tea program
//     (see internal/ui/ui.go and internal/ui/spinner.go);
//   - Ctrl+C cancels the work's context rather than killing the UI, so the
//     program gets to render the interrupted state before exiting;
//   - DoneMsg is sent before waiting for the program to finish, exactly like
//     RunWithRollingLog's trailing done{}.
func Run(ctx context.Context, logPath string, fn func(ctx context.Context, r planner.ProgressReporter) error) error {
	// Non-TTY: there is no program to render against. Hand fn a reporter with
	// nowhere to send so it still works, just without a view.
	// Task 6 replaces this with the plain reporter.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return fn(ctx, NewReporter(nil))
	}

	// Signal to other UI helpers (Spinner, StdPrinter) to suppress direct
	// stdout writes while Bubble Tea owns the terminal.
	_ = os.Setenv("DOCKFORM_TUI_ACTIVE", "1")
	defer func() { _ = os.Unsetenv("DOCKFORM_TUI_ACTIVE") }()

	// Cancellable context so Ctrl+C in the UI stops the work rather than
	// leaving it running headless after the program exits.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cancelCh := make(chan struct{}, 1)

	// Initialise the model with the real terminal width so truncation is
	// correct from the very first render, before any WindowSizeMsg arrives.
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		width = 80
	}

	m := New(nil).WithCancel(cancelCh)
	m.width = width
	p := tea.NewProgram(m, tea.WithOutput(os.Stdout))

	var runErr error
	doneCh := make(chan struct{})
	go func() {
		_, runErr = p.Run()
		close(doneCh)
	}()

	go func() {
		select {
		case <-cancelCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	workErr := fn(ctx, NewReporter(func(msg tea.Msg) { p.Send(msg) }))

	// Tell the view the run is over before waiting for it to quit, exactly
	// like RunWithRollingLog's trailing done{} — the program always gets to
	// render the final frame even though workErr may be non-nil.
	p.Send(DoneMsg{LogPath: logPath})
	<-doneCh

	if workErr != nil {
		return workErr
	}
	return runErr
}

// RunOrPlain runs fn through the inline apply view, unless plain is true — for
// a non-TTY stdout, or when the caller asked for uncolored/non-interactive
// output (e.g. --verbose) — in which case it runs fn directly against a
// reporter that discards every event.
//
// Task 6 replaces the plain branch with a dedicated plain-text renderer; for
// now the events are simply dropped so apply still works end to end without
// a view.
func RunOrPlain(ctx context.Context, plain bool, logPath string, fn func(ctx context.Context, r planner.ProgressReporter) error) error {
	if plain {
		// Task 6 replaces this with the plain reporter.
		return fn(ctx, NewReporter(nil))
	}
	return Run(ctx, logPath, fn)
}
