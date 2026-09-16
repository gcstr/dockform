package applyview

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gcstr/dockform/internal/planner"
)

// countsTowardTotal is the single rule both renderers must share: a
// ResourceStack line is the same work as its services seen at a coarser
// grain, so it must not add to the resource totals either renderer reports.
func TestCountsTowardTotal_ExcludesOnlyStacks(t *testing.T) {
	cases := map[planner.ResourceType]bool{
		planner.ResourceVolume:    true,
		planner.ResourceNetwork:   true,
		planner.ResourceStack:     false,
		planner.ResourceService:   true,
		planner.ResourceFileset:   true,
		planner.ResourceContainer: true,
	}
	for typ, want := range cases {
		ref := planner.ResourceRef{Context: "ctx", Type: typ, Name: "x"}
		if got := countsTowardTotal(ref); got != want {
			t.Errorf("countsTowardTotal(%v) = %v, want %v", typ, got, want)
		}
	}
}

// TestBothRenderersAgreeOnTotal proves Finding 3: seeding the identical set of
// refs — including a stack with services — into both the interactive Model
// and the plain (CI) renderer must produce the same resource total. Before
// the fix, Model.Counts excluded the stack but Plain's Seed/Summarize counted
// every seeded ref, so the same run printed "Applying 3 resources" on a
// terminal and "Applying 4 resources" in CI.
func TestBothRenderersAgreeOnTotal(t *testing.T) {
	vol := planner.ResourceRef{Context: "ctx", Type: planner.ResourceVolume, Name: "data"}
	stack := planner.ResourceRef{Context: "ctx", Type: planner.ResourceStack, Name: "app"}
	svcA := planner.ResourceRef{Context: "ctx", Type: planner.ResourceService, Name: "web", Parent: "app"}
	svcB := planner.ResourceRef{Context: "ctx", Type: planner.ResourceService, Name: "db", Parent: "app"}
	seed := []planner.ResourceRef{vol, stack, svcA, svcB}

	// Interactive (TTY) model.
	m := New(fixedClock(time.Second))
	m = apply(m,
		SeedMsg{Items: seed},
		StartMsg{Ref: vol, Verb: "creating"},
		FinishMsg{Ref: vol, Result: "created"},
		StartMsg{Ref: stack, Verb: "starting"},
		FinishMsg{Ref: stack, Result: "started"},
	)
	_, ttyTotal := m.Counts()

	// Plain (CI) renderer, identical events.
	var buf bytes.Buffer
	p := NewPlain(&buf, fixedClock(time.Second))
	p.Seed(seed)
	p.Start(vol, "creating")
	p.Finish(vol, "created")
	p.Start(stack, "starting")
	p.Finish(stack, "started")
	p.Summarize("")

	wantSeedLine := fmt.Sprintf("Applying %d resources\n", ttyTotal)
	if !strings.Contains(buf.String(), wantSeedLine) {
		t.Fatalf("plain seed line disagrees with the TTY total (%d); want %q, got:\n%s", ttyTotal, wantSeedLine, buf.String())
	}

	wantSummary := fmt.Sprintf("of %d resources applied", ttyTotal)
	if !strings.Contains(buf.String(), wantSummary) {
		t.Fatalf("plain summary disagrees with the TTY total (%d); want %q, got:\n%s", ttyTotal, wantSummary, buf.String())
	}

	if ttyTotal != 3 {
		t.Fatalf("sanity check failed: want 3 counted resources (vol, web, db — not the stack), got %d", ttyTotal)
	}
}
