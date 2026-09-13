package applyview

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gcstr/dockform/internal/planner"
)

func TestReporterSendsOneMessagePerEvent(t *testing.T) {
	var mu sync.Mutex
	var msgs []tea.Msg
	r := NewReporter(func(msg tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		msgs = append(msgs, msg)
	})

	ref := planner.ResourceRef{Context: "ctx", Type: planner.ResourceVolume, Name: "data"}
	r.Seed([]planner.ResourceRef{ref})
	r.Start(ref, "creating")
	r.Detail(ref, "1/2")
	r.Finish(ref, "created")
	r.Fail(ref, errors.New("boom"))

	mu.Lock()
	defer mu.Unlock()
	if len(msgs) != 5 {
		t.Fatalf("got %d messages, want 5", len(msgs))
	}
	if _, ok := msgs[0].(SeedMsg); !ok {
		t.Fatalf("msgs[0] = %T, want SeedMsg", msgs[0])
	}
	if _, ok := msgs[1].(StartMsg); !ok {
		t.Fatalf("msgs[1] = %T, want StartMsg", msgs[1])
	}
	if _, ok := msgs[4].(FailMsg); !ok {
		t.Fatalf("msgs[4] = %T, want FailMsg", msgs[4])
	}
}

// A nil send function must be safe: the non-TTY path constructs a reporter with
// no program behind it.
func TestReporterToleratesNilSend(t *testing.T) {
	r := NewReporter(nil)
	r.Start(planner.ResourceRef{Name: "x"}, "creating")
	r.Finish(planner.ResourceRef{Name: "x"}, "created")
}

// RunOrPlain's plain branch must still hand fn a working, non-nil reporter —
// just one whose events go nowhere — so apply works end to end even before
// Task 6's plain renderer exists.
func TestRunOrPlainUsesPlainPathWhenRequested(t *testing.T) {
	var gotReporter planner.ProgressReporter
	err := RunOrPlain(context.Background(), true, "", func(_ context.Context, r planner.ProgressReporter) error {
		gotReporter = r
		// Must not panic even though nothing is listening.
		r.Seed([]planner.ResourceRef{{Name: "x"}})
		r.Start(planner.ResourceRef{Name: "x"}, "creating")
		r.Finish(planner.ResourceRef{Name: "x"}, "created")
		return nil
	})
	if err != nil {
		t.Fatalf("RunOrPlain: %v", err)
	}
	if gotReporter == nil {
		t.Fatal("expected a non-nil reporter on the plain path")
	}
}

// The plain path must propagate fn's error unchanged, since RunOrPlain is a
// straight pass-through when plain is true.
func TestRunOrPlainPropagatesError(t *testing.T) {
	want := errors.New("boom")
	err := RunOrPlain(context.Background(), true, "", func(context.Context, planner.ProgressReporter) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

// resolveRunError picks what Run returns when the work finishes, mirroring
// ui.RunWithRollingLog's precedence: a cancelled context always wins, because
// the work's own error on a cancelled run is usually an opaque "signal:
// killed" *exec.ExitError from a subprocess SIGKILLed by exec.CommandContext
// (see internal/planner/orchestrate.go's classifyAborted doc), which carries
// no context.Canceled anywhere in its chain. Returning that verbatim would
// defeat root.Execute's errors.Is(err, context.Canceled) check.
func TestResolveRunError(t *testing.T) {
	ctxErr := context.Canceled
	killedErr := &exec.ExitError{}
	plainKilled := errors.New("signal: killed")
	progErr := errors.New("program error")

	tests := []struct {
		name    string
		ctxErr  error
		workErr error
		runErr  error
		want    error
	}{
		{
			name:    "cancelled context with exec.ExitError work error",
			ctxErr:  ctxErr,
			workErr: killedErr,
			runErr:  nil,
			want:    ctxErr,
		},
		{
			name:    "cancelled context with plain signal-killed work error",
			ctxErr:  ctxErr,
			workErr: plainKilled,
			runErr:  nil,
			want:    ctxErr,
		},
		{
			name:    "cancelled context with nil work error",
			ctxErr:  ctxErr,
			workErr: nil,
			runErr:  nil,
			want:    ctxErr,
		},
		{
			name:    "no cancellation, work error returned verbatim",
			ctxErr:  nil,
			workErr: progErr,
			runErr:  nil,
			want:    progErr,
		},
		{
			name:    "no cancellation, no work error, program error returned",
			ctxErr:  nil,
			workErr: nil,
			runErr:  progErr,
			want:    progErr,
		},
		{
			name:    "all nil",
			ctxErr:  nil,
			workErr: nil,
			runErr:  nil,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveRunError(tt.ctxErr, tt.workErr, tt.runErr)
			if !errors.Is(got, tt.want) {
				t.Fatalf("resolveRunError(%v, %v, %v) = %v, want %v", tt.ctxErr, tt.workErr, tt.runErr, got, tt.want)
			}
		})
	}

	// Pin the actual property root.Execute depends on: when the context was
	// cancelled, the result must satisfy errors.Is(result, context.Canceled)
	// even though the work error is an opaque killed-subprocess error with no
	// context.Canceled in its chain.
	got := resolveRunError(context.Canceled, killedErr, nil)
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("errors.Is(resolveRunError(...), context.Canceled) = false, want true; got %v", got)
	}
}
