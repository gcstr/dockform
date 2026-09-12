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

	m := apply(New(fixedClock(time.Second)),
		SeedMsg{Items: []planner.ResourceRef{stack, svc}},
		StartMsg{Ref: stack, Verb: "starting"},
		FailMsg{Ref: stack, Err: errors.New("compose up failed")},
	)

	if got := m.itemFor(svc).State; got != planner.StatePending {
		t.Fatalf("service state = %v, want StatePending", got)
	}
}
