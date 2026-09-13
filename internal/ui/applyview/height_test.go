package applyview

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
)

// bigSeed mirrors the manifest that motivated this feature: three hosts, ~80
// resources. Rendered fully expanded that is ~92 lines, and Bubble Tea keeps only
// the LAST `height` of them — silently discarding the header and the first hosts
// for the whole early phase of a run.
func bigSeed() []planner.ResourceRef {
	var refs []planner.ResourceRef
	for _, ctx := range []string{"hetzner-one", "hetzner-two", "hetzner-three"} {
		for i := 0; i < 8; i++ {
			refs = append(refs, planner.ResourceRef{Context: ctx, Type: planner.ResourceVolume, Name: fmt.Sprintf("vol-%d", i)})
		}
		for i := 0; i < 2; i++ {
			refs = append(refs, planner.ResourceRef{Context: ctx, Type: planner.ResourceNetwork, Name: fmt.Sprintf("net-%d", i)})
		}
		for s := 0; s < 3; s++ {
			stack := fmt.Sprintf("stack-%d", s)
			refs = append(refs, planner.ResourceRef{Context: ctx, Type: planner.ResourceStack, Name: stack})
			for svc := 0; svc < 2; svc++ {
				refs = append(refs, planner.ResourceRef{Context: ctx, Type: planner.ResourceService, Name: fmt.Sprintf("svc-%d", svc), Parent: stack})
			}
		}
		for i := 0; i < 3; i++ {
			refs = append(refs, planner.ResourceRef{Context: ctx, Type: planner.ResourceFileset, Name: fmt.Sprintf("fs-%d", i)})
		}
	}
	return refs
}

func frameHeight(m Model) int {
	return len(strings.Split(strings.TrimRight(ui.StripANSI(m.View()), "\n"), "\n"))
}

func TestFrameFitsTerminalHeightAtSeed(t *testing.T) {
	refs := bigSeed()
	for _, height := range []int{24, 40, 50} {
		m := apply(New(fixedClock(time.Second)),
			tea.WindowSizeMsg{Width: 100, Height: height},
			SeedMsg{Items: refs},
		)
		if got := frameHeight(m); got > height {
			t.Errorf("at height %d the frame is %d lines (%d refs); Bubble Tea would discard the top, including the header",
				height, got, len(refs))
		}
	}
}

func TestFrameFitsTerminalHeightMidRun(t *testing.T) {
	refs := bigSeed()
	m := apply(New(fixedClock(time.Second)),
		tea.WindowSizeMsg{Width: 100, Height: 24},
		SeedMsg{Items: refs},
	)
	// Work under way on the LAST context: the window must follow it, and the
	// frame must still fit.
	for _, ref := range refs {
		if ref.Context == "hetzner-three" && ref.Type == planner.ResourceVolume {
			m = apply(m, StartMsg{Ref: ref, Verb: "creating"})
		}
	}
	if got := frameHeight(m); got > 24 {
		t.Errorf("mid-run frame is %d lines at height 24", got)
	}
	out := ui.StripANSI(m.View())
	if !strings.Contains(out, "Applying") {
		t.Error("header was dropped from the frame; it is the first thing Bubble Tea discards")
	}
	if !strings.Contains(out, "hetzner-three") {
		t.Error("the context with active work is not visible in the frame")
	}
}

// A height of 0 means no WindowSizeMsg has arrived yet; the view must render
// rather than collapse to nothing.
func TestFrameWithoutKnownHeightRendersEverything(t *testing.T) {
	m := apply(New(fixedClock(time.Second)), SeedMsg{Items: bigSeed()})
	if got := frameHeight(m); got < 10 {
		t.Fatalf("with no known height the frame should render in full, got %d lines", got)
	}
}
