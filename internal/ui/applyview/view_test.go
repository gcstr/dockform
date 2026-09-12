package applyview

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
)

var update = flag.Bool("update", false, "rewrite golden files")

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if got != string(want) {
		t.Fatalf("view mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func buildRunningModel() Model {
	stack := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceStack, Name: "linkwarden"}
	svcPg := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceService, Name: "postgres", Parent: "linkwarden"}
	svcApp := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceService, Name: "linkwarden", Parent: "linkwarden"}
	fs := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceFileset, Name: "web_config"}

	m := New(fixedClock(time.Second))
	m = apply(m,
		tea.WindowSizeMsg{Width: 72},
		SeedMsg{Items: []planner.ResourceRef{volRef2("data"), volRef2("cache"), stack, svcPg, svcApp, fs}},
		// Volumes group: fully done, so it must collapse.
		StartMsg{Ref: volRef2("data"), Verb: "creating"},
		FinishMsg{Ref: volRef2("data"), Result: "created"},
		StartMsg{Ref: volRef2("cache"), Verb: "creating"},
		FinishMsg{Ref: volRef2("cache"), Result: "created"},
		// Stacks group: in flight, so it must stay expanded.
		StartMsg{Ref: stack, Verb: "starting"},
		// Filesets group: running with a detail.
		StartMsg{Ref: fs, Verb: "syncing"},
		DetailMsg{Ref: fs, Text: "8/23 files"},
	)
	return m
}

func volRef2(name string) planner.ResourceRef {
	return planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceVolume, Name: name}
}

func TestViewRunningGolden(t *testing.T) {
	checkGolden(t, "running.golden", ui.StripANSI(buildRunningModel().View()))
}

func TestViewFinalWithFailureGolden(t *testing.T) {
	stack := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceStack, Name: "linkwarden"}
	m := apply(buildRunningModel(),
		FailMsg{Ref: stack, Err: errors.New("compose up linkwarden: image pull failed")},
		DoneMsg{LogPath: ".dockform/logs/apply-20260912-180000.log"},
	)

	got := ui.StripANSI(m.View())
	checkGolden(t, "final_with_failure.golden", got)

	// The golden could drift; assert the invariants explicitly too.
	if !strings.Contains(got, "compose up linkwarden: image pull failed") {
		t.Error("final view omits the failure cause")
	}
	if !strings.Contains(got, ".dockform/logs/apply-20260912-180000.log") {
		t.Error("final view omits the run-log path")
	}
}

// A line that was seeded but never started must still be visible as pending in
// the final view — it is how the user sees what did not get done.
func TestFinalViewKeepsPendingLines(t *testing.T) {
	m := apply(New(fixedClock(time.Second)),
		tea.WindowSizeMsg{Width: 72},
		SeedMsg{Items: []planner.ResourceRef{volRef2("touched"), volRef2("untouched")}},
		StartMsg{Ref: volRef2("touched"), Verb: "creating"},
		FinishMsg{Ref: volRef2("touched"), Result: "created"},
		DoneMsg{},
	)
	if !strings.Contains(ui.StripANSI(m.View()), "untouched") {
		t.Error("final view dropped a seeded line that never ran")
	}
}

// TestViewNeverExceedsWidth checks terminal CELL width, not byte length: the
// markers and spinner frames this view uses (✔, ✖, ⠋, ·) are multi-byte UTF-8
// but occupy a single terminal column each, so a byte-length check would flag
// correctly-truncated lines as too long. ansi.StringWidth uses the same
// grapheme-width accounting as ansi.Truncate below, so it measures what
// actually lands in the terminal.
func TestViewNeverExceedsWidth(t *testing.T) {
	m := apply(buildRunningModel(), tea.WindowSizeMsg{Width: 40})
	for _, line := range strings.Split(ui.StripANSI(m.View()), "\n") {
		if w := ansi.StringWidth(line); w > 39 {
			t.Fatalf("line exceeds width-1: %q (%d cells)", line, w)
		}
	}
}
