package applyview

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gcstr/dockform/internal/planner"
)

func TestPlainOutputGolden(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlain(&buf, fixedClock(time.Second))

	vol := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceVolume, Name: "data"}
	stack := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceStack, Name: "linkwarden"}
	fs := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceFileset, Name: "web_config"}

	p.Seed([]planner.ResourceRef{vol, stack, fs})
	p.Start(vol, "creating")
	p.Finish(vol, "created")
	p.Start(fs, "syncing")
	p.Detail(fs, "8/23 files")
	p.Finish(fs, "synced 23 files")
	p.Start(stack, "starting")
	p.Fail(stack, errors.New("compose up linkwarden: image pull failed"))
	p.Summarize(".dockform/logs/apply-20260912-180000.log")

	checkGolden(t, "plain.golden", buf.String())
}

func TestPlainOutputHasNoANSI(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlain(&buf, fixedClock(time.Second))
	ref := planner.ResourceRef{Context: "c", Type: planner.ResourceVolume, Name: "v"}
	p.Start(ref, "creating")
	p.Finish(ref, "created")
	p.Summarize("")

	if bytes.Contains(buf.Bytes(), []byte{0x1b}) {
		t.Fatalf("plain output contains an escape sequence: %q", buf.String())
	}
}

// Seeded-but-never-run lines must be reported, so CI logs show what was skipped.
func TestPlainSummaryListsUnfinishedLines(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlain(&buf, fixedClock(time.Second))
	ran := planner.ResourceRef{Context: "c", Type: planner.ResourceVolume, Name: "ran"}
	skipped := planner.ResourceRef{Context: "c", Type: planner.ResourceVolume, Name: "skipped"}

	p.Seed([]planner.ResourceRef{ran, skipped})
	p.Start(ran, "creating")
	p.Finish(ran, "created")
	p.Summarize("")

	if !strings.Contains(buf.String(), "skipped") {
		t.Fatalf("summary omitted a line that never ran:\n%s", buf.String())
	}
}
