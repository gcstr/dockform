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
	seeded  map[planner.ResourceRef]bool

	milestone map[planner.ResourceRef]int
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
		seeded:  map[planner.ResourceRef]bool{},

		milestone: map[planner.ResourceRef]int{},
	}
}

func label(ref planner.ResourceRef) string {
	name := displayName(ref)
	if ref.Parent != "" {
		return fmt.Sprintf("%s %s/%s", ref.Type, ref.Parent, name)
	}
	return fmt.Sprintf("%s %s", ref.Type, name)
}

// qualifiedLabel prefixes label(ref) with its context, matching the ordering
// the summary lines already use ("<context> <label>"). Apply processes
// contexts in parallel and the same resource name routinely recurs across
// hosts, so every line — not just the summary — must say which context it
// belongs to.
func qualifiedLabel(ref planner.ResourceRef) string {
	return fmt.Sprintf("%s %s", ref.Context, label(ref))
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
	n := 0
	for _, ref := range items {
		p.track(ref)
		p.seeded[ref] = true
		p.state[ref] = planner.StatePending
		// A ResourceStack line is not counted, matching Model.Counts (see
		// countsTowardTotal in cascade.go): apply drives one compose call per
		// stack, so counting the stack too reports more resources than the
		// plan footer the user approved.
		if countsTowardTotal(ref) {
			n++
		}
	}
	_, _ = fmt.Fprintf(p.w, "Applying %d %s\n", n, plural(n, "resource", "resources"))
}

func (p *Plain) Start(ref planner.ResourceRef, verb string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.track(ref)
	p.state[ref] = planner.StateRunning
	p.started[ref] = p.now()
	_, _ = fmt.Fprintf(p.w, "%s: %s\n", qualifiedLabel(ref), verb)
}

func (p *Plain) Detail(ref planner.ResourceRef, text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if text == "" {
		return
	}
	p.track(ref)
	_, _ = fmt.Fprintf(p.w, "%s: %s\n", qualifiedLabel(ref), text)
}

// Progress prints a percentage only when it crosses a milestone — 25, 50, 75 or
// 100 — once per milestone per line. The interactive view redraws a percentage
// in place; a log cannot, and printing every update would bury the run.
func (p *Plain) Progress(ref planner.ResourceRef, percent int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state[ref] != planner.StateRunning {
		return
	}
	reached := 0
	for _, m := range []int{25, 50, 75, 100} {
		if percent >= m {
			reached = m
		}
	}
	if reached == 0 || reached <= p.milestone[ref] {
		return
	}
	p.milestone[ref] = reached
	_, _ = fmt.Fprintf(p.w, "%s: %d%%\n", qualifiedLabel(ref), percent)
}

func (p *Plain) Finish(ref planner.ResourceRef, result string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if result == "" && !p.seeded[ref] {
		// Never in the plan's own seed, and apply reported nothing to do for it
		// (e.g. a fileset whose tree hash still matched at apply time). The
		// Start line already printed above stays — reading its remote index was
		// real, possibly slow, work, and a failure there must stay attributable
		// — but voiding the ref here keeps it out of Summarize's totals, so a
		// run of unchanged filesets doesn't inflate "N of M resources applied"
		// with phantom work that never existed.
		p.forget(ref)
		return
	}
	p.track(ref)
	p.state[ref] = planner.StateDone
	_, _ = fmt.Fprintf(p.w, "%s: %s%s\n", qualifiedLabel(ref), result, p.sinceSuffix(ref))

	// A stack is applied by one compose call, so its seeded services resolve with
	// it — see stackResolves. Without this a fully successful stack reported every
	// one of its services as "not applied" in the summary below.
	for _, child := range p.order {
		if resolvesWithStack(ref, child, p.state[child]) {
			p.state[child] = planner.StateDone
			_, _ = fmt.Fprintf(p.w, "%s: %s\n", qualifiedLabel(child), result)
		}
	}
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
	_, _ = fmt.Fprintf(p.w, "%s: FAILED: %s%s\n", qualifiedLabel(ref), cause, p.sinceSuffix(ref))
}

// forget removes ref as though it had never been tracked at all — used only
// when Finish reports an empty result for a ref that was never seeded, so it
// must not count in Summarize's totals. Callers must hold p.mu.
func (p *Plain) forget(ref planner.ResourceRef) {
	delete(p.state, ref)
	delete(p.started, ref)
	delete(p.causes, ref)
	for i, r := range p.order {
		if r == ref {
			p.order = append(p.order[:i], p.order[i+1:]...)
			break
		}
	}
}

// sinceSuffix renders " (1.0s)" when the line has a start time, or "" when
// there is no start time or the elapsed duration formats as empty (a Finish
// landing at the same clock reading as its Start — formatDuration returns ""
// for d <= 0). Callers must already hold p.mu — it does not lock on its own.
func (p *Plain) sinceSuffix(ref planner.ResourceRef) string {
	start, ok := p.started[ref]
	if !ok {
		return ""
	}
	d := formatDuration(p.now().Sub(start))
	if d == "" {
		return ""
	}
	return " (" + d + ")"
}

// Summarize writes the closing block: a totals line, then every failure (with
// its cause), every line that started but was cut off before it finished or
// failed, and every line that was seeded but never started, so a CI log shows
// exactly what happened without needing the lines around it. An interrupted
// line (StateRunning) is reported distinctly from an untouched one
// (StatePending): the run got partway through it — e.g. a fileset sync that
// already wrote some files — which is a different truth than "nothing ever
// touched this" and matters for anyone deciding whether state was left
// half-applied. Call this exactly once, after the run is over.
func (p *Plain) Summarize(logPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var failed, interrupted, unfinished []planner.ResourceRef
	// total/done mirror Model.Counts: a ResourceStack line is not counted
	// (see countsTowardTotal in cascade.go), so this total agrees with the
	// interactive renderer's for the same run. The failed/interrupted/
	// unfinished listings below stay unfiltered — a failed stack is still
	// useful to name explicitly, it just is not one of the counted totals.
	total, done := 0, 0
	for _, ref := range p.order {
		switch p.state[ref] {
		case planner.StateFailed:
			failed = append(failed, ref)
		case planner.StateDone:
		case planner.StateRunning:
			interrupted = append(interrupted, ref)
		default:
			unfinished = append(unfinished, ref)
		}
		if countsTowardTotal(ref) {
			total++
			if p.state[ref] == planner.StateDone || p.state[ref] == planner.StateFailed {
				done++
			}
		}
	}

	_, _ = fmt.Fprintf(p.w, "\n%d of %d %s applied, %d failed\n", done, total, plural(total, "resource", "resources"), len(failed))
	for _, ref := range failed {
		_, _ = fmt.Fprintf(p.w, "  FAILED %s %s: %s\n", ref.Context, label(ref), p.causes[ref])
	}
	for _, ref := range interrupted {
		_, _ = fmt.Fprintf(p.w, "  interrupted %s %s\n", ref.Context, label(ref))
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
