package applyview

import "github.com/gcstr/dockform/internal/planner"

// SeedMsg declares every line up front.
type SeedMsg struct{ Items []planner.ResourceRef }

// StartMsg moves a line to running with a present-tense verb.
type StartMsg struct {
	Ref  planner.ResourceRef
	Verb string
}

// DetailMsg replaces a running line's sub-status.
type DetailMsg struct {
	Ref  planner.ResourceRef
	Text string
}

// FinishMsg moves a line to done with a past-tense result.
type FinishMsg struct {
	Ref    planner.ResourceRef
	Result string
}

// FailMsg moves a line to failed and records the cause.
type FailMsg struct {
	Ref planner.ResourceRef
	Err error
}

// DoneMsg ends the run and switches the model to its final summary.
type DoneMsg struct{ LogPath string }

// tickMsg drives the shared spinner animation.
type tickMsg struct{}
