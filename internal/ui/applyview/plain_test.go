package applyview

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gcstr/dockform/internal/planner"
)

// TestPlainOutputGolden exercises a run with two contexts sharing a fileset
// name ("web_config" on both hetzner-one and hetzner-two), so the golden
// proves two things at once:
//   - every per-transition line is qualified by context (Finding 1) — without
//     that, the interleaved "fileset web_config: N/M files" Detail lines from
//     the two contexts would be indistinguishable.
//   - the `started` map is keyed per-resource, not by one shared timer
//     (Finding 4): fsOne and fsTwo overlap (fsTwo starts before fsOne
//     finishes) and their elapsed durations differ (3.0s vs 4.0s), which a
//     single shared "last start time" could not reproduce.
//
// It also leaves one resource (net) started but never finished or failed, so
// the summary's "interrupted" bucket (Finding 2) has something to report.
func TestPlainOutputGolden(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlain(&buf, fixedClock(time.Second))

	vol := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceVolume, Name: "data"}
	stack := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceStack, Name: "linkwarden"}
	fsOne := planner.ResourceRef{Context: "hetzner-one", Type: planner.ResourceFileset, Name: "web_config"}
	fsTwo := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceFileset, Name: "web_config"}
	net := planner.ResourceRef{Context: "hetzner-one", Type: planner.ResourceNetwork, Name: "proxy"}

	p.Seed([]planner.ResourceRef{vol, stack, fsOne, fsTwo, net})

	p.Start(fsOne, "syncing") // t=1, started[fsOne]=1
	p.Start(vol, "creating")  // t=2, started[vol]=2
	p.Start(fsTwo, "syncing") // t=3, started[fsTwo]=3 (fsOne still running: overlap)
	p.Detail(fsOne, "8/23 files")
	p.Detail(fsTwo, "14/40 files")
	p.Finish(fsOne, "synced 23 files")                                    // t=4, elapsed 4-1=3.0s
	p.Finish(vol, "created")                                              // t=5, elapsed 5-2=3.0s
	p.Start(stack, "starting")                                            // t=6, started[stack]=6
	p.Finish(fsTwo, "synced 40 files")                                    // t=7, elapsed 7-3=4.0s (differs from fsOne's 3.0s)
	p.Start(net, "starting")                                              // t=8, started[net]=8 — never finished
	p.Fail(stack, errors.New("compose up linkwarden: image pull failed")) // t=9, elapsed 9-6=3.0s

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

// A resource that started and was cut off before it finished or failed is a
// different truth than one nothing ever touched: the summary must label them
// distinctly ("interrupted" vs "not applied"), not collapse both into one
// bucket.
func TestPlainSummaryDistinguishesInterruptedFromUnfinished(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlain(&buf, fixedClock(time.Second))
	started := planner.ResourceRef{Context: "c", Type: planner.ResourceFileset, Name: "cut_off"}
	untouched := planner.ResourceRef{Context: "c", Type: planner.ResourceVolume, Name: "untouched"}

	p.Seed([]planner.ResourceRef{started, untouched})
	p.Start(started, "syncing")
	p.Detail(started, "8/23 files")
	p.Summarize("")

	out := buf.String()
	if !strings.Contains(out, "interrupted c fileset cut_off") {
		t.Fatalf("summary did not mark the started-but-cut-off resource as interrupted:\n%s", out)
	}
	if !strings.Contains(out, "not applied c volume untouched") {
		t.Fatalf("summary did not mark the untouched resource as not applied:\n%s", out)
	}
	if strings.Contains(out, "not applied c fileset cut_off") {
		t.Fatalf("interrupted resource was also reported as not applied:\n%s", out)
	}
}

// formatDuration returns "" for d <= 0, so a Finish landing at the same clock
// reading as its Start must not render the empty-parens "()" suffix.
func TestPlainFinishAtSameInstantOmitsEmptyParens(t *testing.T) {
	var buf bytes.Buffer
	same := time.Date(2026, 9, 12, 18, 0, 0, 0, time.UTC)
	p := NewPlain(&buf, func() time.Time { return same })
	ref := planner.ResourceRef{Context: "c", Type: planner.ResourceVolume, Name: "data"}

	p.Start(ref, "creating")
	p.Finish(ref, "created")

	if strings.Contains(buf.String(), "()") {
		t.Fatalf("finish line has an empty-parens duration suffix:\n%s", buf.String())
	}
	want := "c volume data: creating\nc volume data: created\n"
	if buf.String() != want {
		t.Fatalf("unexpected output:\n got: %q\nwant: %q", buf.String(), want)
	}
}

// A SUCCESSFUL stack must resolve its services. The other golden's only stack
// FAILS, where leaving children pending is correct — which is exactly why this
// bug (every service of a healthy stack reported "not applied" in CI) survived
// until a whole-branch review measured real output.
func TestPlainSuccessfulStackResolvesItsServices(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlain(&buf, fixedClock(time.Second))

	stack := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceStack, Name: "linkwarden"}
	pg := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceService, Name: "postgres", Parent: "linkwarden"}
	app := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceService, Name: "linkwarden", Parent: "linkwarden"}
	// Same service name under a DIFFERENT stack: it must not be swept up.
	other := planner.ResourceRef{Context: "hetzner-two", Type: planner.ResourceService, Name: "postgres", Parent: "authelia"}

	p.Seed([]planner.ResourceRef{stack, pg, app, other})
	p.Start(stack, "starting")
	p.Finish(stack, "started")
	p.Summarize("")

	out := buf.String()
	if strings.Contains(out, "not applied hetzner-two service linkwarden/postgres") {
		t.Errorf("a successful stack's service was reported not applied:\n%s", out)
	}
	if !strings.Contains(out, "not applied hetzner-two service authelia/postgres") {
		t.Errorf("a same-named service under another stack should NOT have resolved:\n%s", out)
	}
	if !strings.Contains(out, "3 of 4 changes applied, 0 failed") {
		t.Errorf("completion count wrong; want 3 of 4:\n%s", out)
	}
}
