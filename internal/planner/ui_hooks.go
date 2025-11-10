package planner

import "github.com/gcstr/dockform/internal/ui"

// ProgressReporter exposes the subset of spinner behavior needed by planner helpers.
// It updates the spinner label to show the current task.
type ProgressReporter interface {
	SetAction(action string)
}

type spinnerAdapter struct {
	inner  *ui.Spinner
	prefix string // Stores initial label (e.g., "Applying") to prepend to actions
}

func (s *spinnerAdapter) SetAction(action string) {
	if s == nil || s.inner == nil {
		return
	}
	// Prepend the prefix with " -> " to show: "Applying -> creating volume data"
	if s.prefix != "" {
		s.inner.SetLabel(s.prefix + " -> " + action)
	} else {
		s.inner.SetLabel(action)
	}
}

func newProgressReporter(spinner *ui.Spinner, prefix string) ProgressReporter {
	if spinner == nil {
		return nil
	}
	return &spinnerAdapter{inner: spinner, prefix: prefix}
}
