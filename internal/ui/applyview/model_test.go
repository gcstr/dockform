package applyview

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gcstr/dockform/internal/planner"
)

// fixedClock returns a clock that advances by step on every call.
func fixedClock(step time.Duration) func() time.Time {
	base := time.Date(2026, 9, 12, 18, 0, 0, 0, time.UTC)
	n := 0
	return func() time.Time {
		n++
		return base.Add(time.Duration(n) * step)
	}
}

func volRef(name string) planner.ResourceRef {
	return planner.ResourceRef{Context: "ctx", Type: planner.ResourceVolume, Name: name}
}

func apply(m Model, msgs ...tea.Msg) Model {
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func TestSeedCreatesPendingItems(t *testing.T) {
	m := apply(New(fixedClock(time.Second)), SeedMsg{Items: []planner.ResourceRef{volRef("a"), volRef("b")}})

	done, total := m.Counts()
	if done != 0 || total != 2 {
		t.Fatalf("Counts() = (%d, %d), want (0, 2)", done, total)
	}
	if got := m.itemFor(volRef("a")).State; got != planner.StatePending {
		t.Fatalf("state = %v, want StatePending", got)
	}
}

func TestStartThenFinishCountsAsDone(t *testing.T) {
	m := apply(New(fixedClock(time.Second)),
		SeedMsg{Items: []planner.ResourceRef{volRef("a"), volRef("b")}},
		StartMsg{Ref: volRef("a"), Verb: "creating"},
		FinishMsg{Ref: volRef("a"), Result: "created"},
	)

	done, total := m.Counts()
	if done != 1 || total != 2 {
		t.Fatalf("Counts() = (%d, %d), want (1, 2)", done, total)
	}
	it := m.itemFor(volRef("a"))
	if it.State != planner.StateDone || it.Result != "created" {
		t.Fatalf("item = %+v, want StateDone/created", it)
	}
	if it.Elapsed() <= 0 {
		t.Fatal("Elapsed() should be positive after Finish")
	}
}

func TestUnseededRefIsAppendedAsDiscovered(t *testing.T) {
	m := apply(New(fixedClock(time.Second)),
		SeedMsg{Items: []planner.ResourceRef{volRef("a")}},
		StartMsg{Ref: planner.ResourceRef{Context: "ctx", Type: planner.ResourceService, Name: "web"}, Verb: "restarting"},
	)

	_, total := m.Counts()
	if total != 2 {
		t.Fatalf("total = %d, want 2 (seeded + discovered)", total)
	}
	it := m.itemFor(planner.ResourceRef{Context: "ctx", Type: planner.ResourceService, Name: "web"})
	if it == nil {
		t.Fatal("discovered ref was dropped")
	}
	if !it.Discovered {
		t.Fatal("discovered ref not flagged as discovered")
	}
}

func TestGroupCollapsesOnlyWhenAllDoneAndNoneFailed(t *testing.T) {
	seed := SeedMsg{Items: []planner.ResourceRef{volRef("a"), volRef("b")}}

	partial := apply(New(fixedClock(time.Second)), seed,
		StartMsg{Ref: volRef("a"), Verb: "creating"},
		FinishMsg{Ref: volRef("a"), Result: "created"},
	)
	if partial.groupFor(volRef("a")).collapsed() {
		t.Fatal("group collapsed while an item was still pending")
	}

	all := apply(partial,
		StartMsg{Ref: volRef("b"), Verb: "creating"},
		FinishMsg{Ref: volRef("b"), Result: "created"},
	)
	if !all.groupFor(volRef("a")).collapsed() {
		t.Fatal("group did not collapse once every item was done")
	}

	failed := apply(New(fixedClock(time.Second)), seed,
		StartMsg{Ref: volRef("a"), Verb: "creating"},
		FinishMsg{Ref: volRef("a"), Result: "created"},
		StartMsg{Ref: volRef("b"), Verb: "creating"},
		FailMsg{Ref: volRef("b"), Err: errors.New("no space left on device")},
	)
	if failed.groupFor(volRef("b")).collapsed() {
		t.Fatal("a group containing a failure must stay expanded")
	}
	if len(failed.Failures()) != 1 {
		t.Fatalf("Failures() returned %d, want 1", len(failed.Failures()))
	}
}

func TestFinishingAStackResolvesItsServices(t *testing.T) {
	stack := planner.ResourceRef{Context: "ctx", Type: planner.ResourceStack, Name: "linkwarden"}
	svc := planner.ResourceRef{Context: "ctx", Type: planner.ResourceService, Name: "postgres", Parent: "linkwarden"}

	m := apply(New(fixedClock(time.Second)),
		SeedMsg{Items: []planner.ResourceRef{stack, svc}},
		StartMsg{Ref: stack, Verb: "starting"},
		FinishMsg{Ref: stack, Result: "started"},
	)

	if got := m.itemFor(svc).State; got != planner.StateDone {
		t.Fatalf("service state = %v, want StateDone (services resolve with their stack)", got)
	}
}

// A failing stack must NOT claim its services failed — only the stack line did.
func TestFailingAStackLeavesServicesPending(t *testing.T) {
	stack := planner.ResourceRef{Context: "ctx", Type: planner.ResourceStack, Name: "linkwarden"}
	svc := planner.ResourceRef{Context: "ctx", Type: planner.ResourceService, Name: "postgres", Parent: "linkwarden"}
	failErr := errors.New("compose up failed")

	m := apply(New(fixedClock(time.Second)),
		SeedMsg{Items: []planner.ResourceRef{stack, svc}},
		StartMsg{Ref: stack, Verb: "starting"},
		FailMsg{Ref: stack, Err: failErr},
	)

	if got := m.itemFor(svc).State; got != planner.StatePending {
		t.Fatalf("service state = %v, want StatePending", got)
	}

	// The stack's OWN line must have actually transitioned to StateFailed and
	// carry the error — without this assertion, the whole `case FailMsg:`
	// branch in Update could be deleted and this test would still pass.
	stackItem := m.itemFor(stack)
	if stackItem.State != planner.StateFailed {
		t.Fatalf("stack state = %v, want StateFailed", stackItem.State)
	}
	if stackItem.Err != failErr {
		t.Fatalf("stack err = %v, want %v", stackItem.Err, failErr)
	}
}

// TestFinishCascadeDoesNotCrossStackOrContextBoundary guards the Finish
// cascade (Update's `case FinishMsg` for ResourceStack) against two distinct
// collisions it must not resolve:
//   - a different stack in the SAME context that happens to have a
//     same-named service ("web" under both "alpha" and "beta" in "ctx1")
//   - the SAME stack name running in a DIFFERENT context ("shared" in both
//     "ctx1" and "ctx2")
//
// ResourceRef.Context plus the loop's Parent == stack name filter exist
// precisely so neither collision resolves a service that does not belong to
// the stack that finished.
// TestDiscoveredNoOpDoesNotInflateTotal proves Finding I5: a ref the plan
// never seeded, which apply then discovers needs no work at all (an empty
// Finish result), must not permanently grow the header's total. The early
// Start is deliberate and must still create the line — reading remote state
// is real work that can fail — but Finish("") for a Discovered item voids it
// again rather than counting it.
func TestDiscoveredNoOpDoesNotInflateTotal(t *testing.T) {
	discovered := planner.ResourceRef{Context: "ctx", Type: planner.ResourceFileset, Name: "unchanged"}

	m := apply(New(fixedClock(time.Second)), SeedMsg{Items: []planner.ResourceRef{volRef("real")}})
	_, seededTotal := m.Counts()

	m = apply(m,
		StartMsg{Ref: discovered, Verb: "syncing"},
		FinishMsg{Ref: discovered, Result: ""},
	)
	_, finalTotal := m.Counts()

	if finalTotal != seededTotal {
		t.Fatalf("total after a discovered no-op = %d, want it unchanged from the post-seed total %d", finalTotal, seededTotal)
	}
	if m.itemFor(discovered) != nil {
		t.Fatal("a discovered no-op must be dropped entirely, not merely excluded from the count")
	}
}

// TestDiscoveredRealWorkStillCounts guards the other side of the same fix: an
// empty Finish result is the drop signal, but any non-empty result — even for
// a Discovered item — must still be counted normally. Otherwise fixing I5
// would risk swallowing genuinely discovered work.
func TestDiscoveredRealWorkStillCounts(t *testing.T) {
	discovered := planner.ResourceRef{Context: "ctx", Type: planner.ResourceService, Name: "web"}

	m := apply(New(fixedClock(time.Second)),
		SeedMsg{Items: []planner.ResourceRef{volRef("real")}},
		StartMsg{Ref: discovered, Verb: "restarting"},
		FinishMsg{Ref: discovered, Result: "restarted"},
	)

	_, total := m.Counts()
	if total != 2 {
		t.Fatalf("total = %d, want 2 (seeded + discovered-with-real-work)", total)
	}
	it := m.itemFor(discovered)
	if it == nil || it.State != planner.StateDone || it.Result != "restarted" {
		t.Fatalf("discovered item with a real result was dropped or mis-recorded: %+v", it)
	}
}

func TestFinishCascadeDoesNotCrossStackOrContextBoundary(t *testing.T) {
	alphaStack := planner.ResourceRef{Context: "ctx1", Type: planner.ResourceStack, Name: "alpha"}
	alphaWeb := planner.ResourceRef{Context: "ctx1", Type: planner.ResourceService, Name: "web", Parent: "alpha"}
	betaStack := planner.ResourceRef{Context: "ctx1", Type: planner.ResourceStack, Name: "beta"}
	betaWeb := planner.ResourceRef{Context: "ctx1", Type: planner.ResourceService, Name: "web", Parent: "beta"}
	sharedStackCtx1 := planner.ResourceRef{Context: "ctx1", Type: planner.ResourceStack, Name: "shared"}
	sharedAppCtx1 := planner.ResourceRef{Context: "ctx1", Type: planner.ResourceService, Name: "app", Parent: "shared"}
	sharedStackCtx2 := planner.ResourceRef{Context: "ctx2", Type: planner.ResourceStack, Name: "shared"}
	sharedAppCtx2 := planner.ResourceRef{Context: "ctx2", Type: planner.ResourceService, Name: "app", Parent: "shared"}

	m := apply(New(fixedClock(time.Second)),
		SeedMsg{Items: []planner.ResourceRef{
			alphaStack, alphaWeb,
			betaStack, betaWeb,
			sharedStackCtx1, sharedAppCtx1,
			sharedStackCtx2, sharedAppCtx2,
		}},
		StartMsg{Ref: alphaStack, Verb: "starting"},
		FinishMsg{Ref: alphaStack, Result: "started"},
	)

	if got := m.itemFor(alphaWeb).State; got != planner.StateDone {
		t.Fatalf("alpha's own service state = %v, want StateDone", got)
	}

	pending := map[string]planner.ResourceRef{
		"ctx1/beta's web (same context, different stack, same service name)": betaWeb,
		"ctx1/shared's app (unrelated stack)":                                sharedAppCtx1,
		"ctx2/shared's app (same stack name, different context)":             sharedAppCtx2,
	}
	for desc, ref := range pending {
		if got := m.itemFor(ref).State; got != planner.StatePending {
			t.Fatalf("%s state = %v, want StatePending (finish cascade crossed a boundary)", desc, got)
		}
	}
}

// The summary's parts must account for the whole. A reader given
// "Applied 2/5 changes · 1 failed" reasonably tries 2 + 1 + something = 5 and
// gets nowhere: the failed line is a stack, which Counts excludes as the coarse
// view of its own services, and three lines were left unexplained. applied +
// interrupted + notApplied is the split that actually reconciles.
func TestOutcomesAccountForEveryCountedChange(t *testing.T) {
	stack := planner.ResourceRef{Context: "ctx", Type: planner.ResourceStack, Name: "app"}
	svcA := planner.ResourceRef{Context: "ctx", Type: planner.ResourceService, Name: "a", Parent: "app"}
	svcB := planner.ResourceRef{Context: "ctx", Type: planner.ResourceService, Name: "b", Parent: "app"}
	vol := planner.ResourceRef{Context: "ctx", Type: planner.ResourceVolume, Name: "data"}
	fs := planner.ResourceRef{Context: "ctx", Type: planner.ResourceFileset, Name: "cfg"}

	m := apply(New(fixedClock(time.Second)),
		SeedMsg{Items: []planner.ResourceRef{vol, stack, svcA, svcB, fs}},
		StartMsg{Ref: vol, Verb: "creating"},
		FinishMsg{Ref: vol, Result: "created"},
		StartMsg{Ref: fs, Verb: "syncing"}, // still running when the run ends
		StartMsg{Ref: stack, Verb: "starting"},
		FailMsg{Ref: stack, Err: errors.New("image pull failed")},
		DoneMsg{},
	)

	_, total := m.Counts()
	applied, interrupted, notApplied := m.Outcomes()
	if applied+interrupted+notApplied != total {
		t.Fatalf("outcomes do not account for the total: %d applied + %d interrupted + %d not applied != %d",
			applied, interrupted, notApplied, total)
	}
	// The specific shape a reader would check by hand.
	if applied != 1 || interrupted != 1 || notApplied != 2 || total != 4 {
		t.Fatalf("got applied=%d interrupted=%d notApplied=%d total=%d; want 1/1/2 of 4",
			applied, interrupted, notApplied, total)
	}
	// And the failed stack is reported, without being one of the four.
	if len(m.Failures()) != 1 {
		t.Fatalf("expected the failed stack to still be reported, got %d failures", len(m.Failures()))
	}
}
