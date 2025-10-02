package planner

import "github.com/gcstr/dockform/internal/ui"

// ProgressReporter exposes the subset of progress bar behavior needed by planner helpers.
type ProgressReporter interface {
	Start(total int)
	SetAction(action string)
	Increment()
}

type progressAdapter struct {
	inner *ui.Progress
}

func (p *progressAdapter) Start(total int) {
	if p == nil || p.inner == nil {
		return
	}
	p.inner.Start(total)
}

func (p *progressAdapter) SetAction(action string) {
	if p == nil || p.inner == nil {
		return
	}
	p.inner.SetAction(action)
}

func (p *progressAdapter) Increment() {
	if p == nil || p.inner == nil {
		return
	}
	p.inner.Increment()
}

func newProgressReporter(p *ui.Progress) ProgressReporter {
	if p == nil {
		return nil
	}
	return &progressAdapter{inner: p}
}
