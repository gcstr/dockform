package applyview

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gcstr/dockform/internal/planner"
)

// Plain writes one line per state transition, with no ANSI and no cursor
// movement, so CI output and piped output are byte-identical to each other.
type Plain struct {
	mu      sync.Mutex
	w       io.Writer
	now     func() time.Time
	started map[planner.ResourceRef]time.Time
	order   []planner.ResourceRef
	state   map[planner.ResourceRef]planner.ResourceState
	causes  map[planner.ResourceRef]string
}

// NewPlain creates a plain reporter writing to w. now is injected for tests.
func NewPlain(w io.Writer, now func() time.Time) *Plain {
	if now == nil {
		now = time.Now
	}
	return &Plain{
		w:       w,
		now:     now,
		started: map[planner.ResourceRef]time.Time{},
		state:   map[planner.ResourceRef]planner.ResourceState{},
		causes:  map[planner.ResourceRef]string{},
	}
}

func label(ref planner.ResourceRef) string {
	if ref.Parent != "" {
		return fmt.Sprintf("%s %s/%s", ref.Type, ref.Parent, ref.Name)
	}
	return fmt.Sprintf("%s %s", ref.Type, ref.Name)
}

// track records ref in display order the first time it is seen. Callers must
// hold p.mu.
func (p *Plain) track(ref planner.ResourceRef) {
	if _, seen := p.state[ref]; !seen {
		p.order = append(p.order, ref)
	}
}

func (p *Plain) Seed(items []planner.ResourceRef) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ref := range items {
		p.track(ref)
		p.state[ref] = planner.StatePending
	}
	_, _ = fmt.Fprintf(p.w, "Applying %d changes\n", len(items))
}

func (p *Plain) Start(ref planner.ResourceRef, verb string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.track(ref)
	p.state[ref] = planner.StateRunning
	p.started[ref] = p.now()
	_, _ = fmt.Fprintf(p.w, "%s: %s\n", label(ref), verb)
}

func (p *Plain) Detail(ref planner.ResourceRef, text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.track(ref)
	_, _ = fmt.Fprintf(p.w, "%s: %s\n", label(ref), text)
}

func (p *Plain) Finish(ref planner.ResourceRef, result string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.track(ref)
	p.state[ref] = planner.StateDone
	_, _ = fmt.Fprintf(p.w, "%s: %s%s\n", label(ref), result, p.sinceSuffix(ref))
}

func (p *Plain) Fail(ref planner.ResourceRef, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.track(ref)
	p.state[ref] = planner.StateFailed
	cause := "failed"
	if err != nil {
		cause = err.Error()
	}
	p.causes[ref] = cause
	_, _ = fmt.Fprintf(p.w, "%s: FAILED: %s%s\n", label(ref), cause, p.sinceSuffix(ref))
}

// sinceSuffix renders " (1.0s)" when the line has a start time. Callers must
// already hold p.mu — it does not lock on its own.
func (p *Plain) sinceSuffix(ref planner.ResourceRef) string {
	start, ok := p.started[ref]
	if !ok {
		return ""
	}
	return " (" + formatDuration(p.now().Sub(start)) + ")"
}

// Summarize writes the closing block: a totals line, then every failure (with
// its cause) and every line that was seeded but never finished, so a CI log
// shows exactly what happened without needing the lines around it. Call this
// exactly once, after the run is over.
func (p *Plain) Summarize(logPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var failed, unfinished []planner.ResourceRef
	for _, ref := range p.order {
		switch p.state[ref] {
		case planner.StateFailed:
			failed = append(failed, ref)
		case planner.StateDone:
		default:
			unfinished = append(unfinished, ref)
		}
	}

	_, _ = fmt.Fprintf(p.w, "\n%d of %d changes applied, %d failed\n", len(p.order)-len(failed)-len(unfinished), len(p.order), len(failed))
	for _, ref := range failed {
		_, _ = fmt.Fprintf(p.w, "  FAILED %s %s: %s\n", ref.Context, label(ref), p.causes[ref])
	}
	for _, ref := range unfinished {
		_, _ = fmt.Fprintf(p.w, "  not applied %s %s\n", ref.Context, label(ref))
	}
	if logPath != "" {
		_, _ = fmt.Fprintf(p.w, "  log: %s\n", logPath)
	}
}

// RunPlain is the non-TTY counterpart of Run: it hands fn a Plain reporter
// writing to w, then always prints the closing summary, even when fn returns
// an error, so a failed or cancelled run still ends with a full account of
// what happened.
func RunPlain(ctx context.Context, w io.Writer, logPath string, fn func(ctx context.Context, r planner.ProgressReporter) error) error {
	p := NewPlain(w, nil)
	err := fn(ctx, p)
	p.Summarize(logPath)
	return err
}
