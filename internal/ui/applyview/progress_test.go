package applyview

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gcstr/dockform/internal/planner"
)

var pgRef = planner.ResourceRef{Context: "ctx", Type: planner.ResourceService, Name: "db", Parent: "app"}

func TestModel_ProgressRendersVerbAndPercent(t *testing.T) {
	m := apply(New(fixedClock(time.Second)),
		tea.WindowSizeMsg{Width: 80},
		SeedMsg{Items: []planner.ResourceRef{pgRef}},
		StartMsg{Ref: pgRef, Verb: "pulling"},
		ProgressMsg{Ref: pgRef, Percent: 45},
	)
	if got := m.itemFor(pgRef).status(); got != "pulling 45%" {
		t.Fatalf("status = %q, want %q", got, "pulling 45%")
	}
}

// A later phase's detail replaces the percentage of the earlier pull.
func TestModel_DetailTakesPrecedenceOverPercent(t *testing.T) {
	m := apply(New(fixedClock(time.Second)),
		tea.WindowSizeMsg{Width: 80},
		SeedMsg{Items: []planner.ResourceRef{pgRef}},
		StartMsg{Ref: pgRef, Verb: "pulling"},
		ProgressMsg{Ref: pgRef, Percent: 100},
		DetailMsg{Ref: pgRef, Text: "creating…"},
	)
	if got := m.itemFor(pgRef).status(); got != "creating…" {
		t.Fatalf("status = %q, want %q", got, "creating…")
	}
}

// Progress must never create a line, and never revive a finished one.
func TestModel_ProgressNeverCreatesOrRevivesALine(t *testing.T) {
	unseeded := planner.ResourceRef{Context: "ctx", Type: planner.ResourceService, Name: "ghost", Parent: "app"}
	m := apply(New(fixedClock(time.Second)),
		tea.WindowSizeMsg{Width: 80},
		SeedMsg{Items: []planner.ResourceRef{pgRef}},
		ProgressMsg{Ref: unseeded, Percent: 10},
		StartMsg{Ref: pgRef, Verb: "pulling"},
		FinishMsg{Ref: pgRef, Result: "created"},
		ProgressMsg{Ref: pgRef, Percent: 50},
	)
	if m.itemFor(unseeded) != nil {
		t.Fatal("Progress created a line for an unseeded ref")
	}
	if got := m.itemFor(pgRef).status(); got != "created" {
		t.Fatalf("status = %q, want %q", got, "created")
	}
}

func plainPercentLines(t *testing.T, percents ...int) []string {
	t.Helper()
	var buf bytes.Buffer
	p := NewPlain(&buf, fixedClock(time.Second))
	p.Seed([]planner.ResourceRef{pgRef})
	p.Start(pgRef, "pulling")
	for _, pct := range percents {
		p.Progress(pgRef, pct)
	}
	var out []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "%") {
			out = append(out, line)
		}
	}
	return out
}

func TestPlainProgress_PrintsFourMilestonesForAFullRun(t *testing.T) {
	var all []int
	for i := 0; i <= 100; i++ {
		all = append(all, i)
	}
	if got := plainPercentLines(t, all...); len(got) != 4 {
		t.Fatalf("got %d percentage lines, want 4: %q", len(got), got)
	}
}

// Milestones are crossed, not hit exactly: a jump prints once.
func TestPlainProgress_JumpPrintsOncePerCrossing(t *testing.T) {
	got := plainPercentLines(t, 20, 60, 100)
	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(got), got)
	}
	if !strings.HasSuffix(got[0], ": 60%") || !strings.HasSuffix(got[1], ": 100%") {
		t.Fatalf("got %q, want lines ending 60%% and 100%%", got)
	}
}

func TestPlainProgress_IgnoresALineThatIsNotRunning(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlain(&buf, fixedClock(time.Second))
	p.Seed([]planner.ResourceRef{pgRef})
	p.Progress(pgRef, 100)
	if strings.Contains(buf.String(), "%") {
		t.Fatalf("progress printed for a pending line: %q", buf.String())
	}
}

// The tracker clears a stack's wait with an empty detail. The interactive view
// falls back to the verb; a CI log must not print an empty line for it.
func TestPlainDetail_EmptyTextPrintsNothing(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlain(&buf, fixedClock(time.Second))
	p.Seed([]planner.ResourceRef{pgRef})
	p.Start(pgRef, "starting")
	before := buf.Len()
	p.Detail(pgRef, "")
	if buf.Len() != before {
		t.Fatalf("empty detail printed: %q", buf.String()[before:])
	}
}
