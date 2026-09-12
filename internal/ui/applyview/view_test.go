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

// TestInterruptedItemDoesNotAnimate proves Finding 2: a StateRunning item
// whose run ends before it finishes or fails did not succeed, fail, or stay
// pending — it started and was cut off, and the final view must say so
// plainly with a marker that has actually stopped. A frozen spinner there
// would claim the run is still making progress on it, which is no longer
// true.
func TestInterruptedItemDoesNotAnimate(t *testing.T) {
	m := apply(New(fixedClock(time.Second)),
		tea.WindowSizeMsg{Width: 72},
		SeedMsg{Items: []planner.ResourceRef{volRef2("slow")}},
		StartMsg{Ref: volRef2("slow"), Verb: "creating"},
		DoneMsg{},
	)

	got := ui.StripANSI(m.View())
	if !strings.Contains(got, "interrupted") {
		t.Errorf("final view does not report the cut-off item as interrupted:\n%s", got)
	}
	if !strings.Contains(got, markerInterrupted) {
		t.Errorf("final view does not use the interrupted marker %q for the cut-off item:\n%s", markerInterrupted, got)
	}
	for _, frame := range spinnerFrames {
		if strings.Contains(got, frame) {
			t.Errorf("final view still animates spinner frame %q for a cut-off item:\n%s", frame, got)
		}
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

// TestFinalFrameNeverExceedsWidth proves Finding 1: the final frame must flow
// through the same width truncation as the running frame. A character landing
// in the terminal's last column triggers pending autowrap and breaks Bubble
// Tea's cursor-up line accounting (see internal/ui/rollinglog.go) — the final
// frame is redrawn through that exact same inline path one last time before
// tea.Quit takes effect, so it needs the identical protection.
//
// TestViewNeverExceedsWidth above can't catch a regression here: it only ever
// drives buildRunningModel(), which never leaves stateRunning, so it never
// reaches the branch this test targets.
func TestFinalFrameNeverExceedsWidth(t *testing.T) {
	stack := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceStack, Name: "linkwarden"}
	longErr := errors.New(`compose up linkwarden: failed to pull image ghcr.io/linkwarden/linkwarden:latest: ` +
		`Get "https://ghcr.io/v2/linkwarden/linkwarden/manifests/latest": dial tcp: lookup ghcr.io on 10.0.0.1:53: ` +
		`no such host (check your DNS configuration and proxy settings, then retry)`)
	longLogPath := ".dockform/logs/apply-20260912-180000-hetzner-two-linkwarden-compose-up-a-very-long-run-identifier-for-testing.log"

	m := apply(buildRunningModel(),
		tea.WindowSizeMsg{Width: 40},
		FailMsg{Ref: stack, Err: longErr},
		DoneMsg{LogPath: longLogPath},
	)

	for i, line := range strings.Split(ui.StripANSI(m.View()), "\n") {
		if w := ansi.StringWidth(line); w > 39 {
			t.Errorf("line %d exceeds width-1: %q (%d cells)", i, line, w)
		}
	}
}
