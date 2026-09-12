package applyview

import (
	"context"
	"errors"
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
