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
//   - DoneMsg is always sent before waiting for the program to finish —
//     unlike RunWithRollingLog, which skips its trailing done{} on
//     cancellation, Run sends DoneMsg even then, so the renderer still gets
//     to switch to the final "■ interrupted" frame instead of leaving live
//     spinners on screen;
//   - a cancelled context wins over fn's returned error when choosing what
//     to return, exactly like RunWithRollingLog's ctx.Err() check — see
//     resolveRunError.
func Run(ctx context.Context, logPath string, fn func(ctx context.Context, r planner.ProgressReporter) error) error {
	// Non-TTY: there is no program to render against. Route through the plain
	// reporter so piped and CI runs still get one line per transition.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return RunPlain(ctx, os.Stdout, logPath, fn)
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
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		width = 80
	}
	if err != nil || height <= 0 {
		// 0 means "unknown" to the view, which then renders everything. Better to
		// assume a small terminal than to hand Bubble Tea a frame it will silently
		// crop from the top.
		height = 24
	}

	m := New(nil).WithCancel(cancelCh)
	m.width = width
	// Seed the height too, so the very FIRST frame is already bounded. Bubble Tea
	// sends a WindowSizeMsg of its own, but not before the first paint — and the
	// first paint of a large manifest is precisely the tall one.
	m.height = height
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
	// render the final frame even though workErr may be non-nil. Unlike
	// RunWithRollingLog, DoneMsg is sent even when ctx was cancelled: the
	// renderer needs it to switch to the final frame where unfinished items
	// render as "■ interrupted" (see TestInterruptedItemDoesNotAnimate)
	// instead of leaving live spinners on screen for work that has stopped.
	p.Send(DoneMsg{LogPath: logPath})
	<-doneCh

	return resolveRunError(ctx.Err(), workErr, runErr)
}

// resolveRunError picks what Run returns. A cancelled context wins over both
// other errors: the work's error on a cancelled run is usually a subprocess
// killed by the signal (an *exec.ExitError carrying no context.Canceled), so
// returning it verbatim would defeat root.Execute's errors.Is(err,
// context.Canceled) check and exit with the wrong code. Mirrors
// ui.RunWithRollingLog.
func resolveRunError(ctxErr, workErr, runErr error) error {
	if ctxErr != nil {
		return ctxErr
	}
	if workErr != nil {
		return workErr
	}
	return runErr
}

// RunOrPlain runs fn through the inline apply view, unless plain is true — for
// a non-TTY stdout, or when the caller asked for uncolored/non-interactive
// output (e.g. --verbose) — in which case it runs fn through the plain
// reporter, which writes one line per transition with no ANSI.
func RunOrPlain(ctx context.Context, plain bool, logPath string, fn func(ctx context.Context, r planner.ProgressReporter) error) error {
	if plain {
		return RunPlain(ctx, os.Stdout, logPath, fn)
	}
	return Run(ctx, logPath, fn)
}
