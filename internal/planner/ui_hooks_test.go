package planner

import (
	"errors"
	"sync"
	"testing"
)

// recordingReporter captures the event sequence. Mutex-guarded because apply
// processes contexts in parallel.
type recordedEvent struct {
	Kind string // "seed", "start", "detail", "finish", "fail"
	Ref  ResourceRef
	Text string
}

type recordingReporter struct {
	mu        sync.Mutex
	events    []recordedEvent
	seedCalls int // number of times Seed was invoked, regardless of item count
}

func (r *recordingReporter) Seed(items []ResourceRef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seedCalls++
	for _, it := range items {
		r.events = append(r.events, recordedEvent{Kind: "seed", Ref: it})
	}
}

func (r *recordingReporter) Start(ref ResourceRef, verb string) {
	r.add("start", ref, verb)
}

func (r *recordingReporter) Detail(ref ResourceRef, text string) {
	r.add("detail", ref, text)
}

func (r *recordingReporter) Finish(ref ResourceRef, result string) {
	r.add("finish", ref, result)
}

func (r *recordingReporter) Fail(ref ResourceRef, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	r.add("fail", ref, msg)
}

func (r *recordingReporter) add(kind string, ref ResourceRef, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{Kind: kind, Ref: ref, Text: text})
}

// kinds returns the event kinds recorded for one ref, in order.
func (r *recordingReporter) kinds(ref ResourceRef) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.events {
		if e.Ref == ref {
			out = append(out, e.Kind)
		}
	}
	return out
}

func TestOrNopNeverReturnsNil(t *testing.T) {
	r := orNop(nil)
	if r == nil {
		t.Fatal("orNop(nil) returned nil; call sites rely on it being safe to call")
	}
	// Must not panic.
	r.Seed([]ResourceRef{{Name: "x"}})
	r.Start(ResourceRef{Name: "x"}, "creating")
	r.Detail(ResourceRef{Name: "x"}, "1/2")
	r.Finish(ResourceRef{Name: "x"}, "created")
	r.Fail(ResourceRef{Name: "x"}, errors.New("boom"))
}

func TestOrNopPassesThroughRealReporter(t *testing.T) {
	rec := &recordingReporter{}
	if got := orNop(rec); got != ProgressReporter(rec) {
		t.Fatalf("orNop returned a different reporter than the one passed in")
	}
}

func TestRecordingReporterTracksPerRefSequence(t *testing.T) {
	rec := &recordingReporter{}
	ref := ResourceRef{Context: "ctx", Type: ResourceVolume, Name: "data"}
	other := ResourceRef{Context: "ctx", Type: ResourceVolume, Name: "cache"}

	rec.Seed([]ResourceRef{ref, other})
	rec.Start(ref, "creating")
	rec.Finish(ref, "created")

	got := rec.kinds(ref)
	want := []string{"seed", "start", "finish"}
	if len(got) != len(want) {
		t.Fatalf("kinds(ref) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds(ref) = %v, want %v", got, want)
		}
	}
	if k := rec.kinds(other); len(k) != 1 || k[0] != "seed" {
		t.Fatalf("kinds(other) = %v, want [seed]", k)
	}
}

func TestWithProgressReporterRoundTrips(t *testing.T) {
	rec := &recordingReporter{}
	p := New().WithProgressReporter(rec)
	if p.reporter != ProgressReporter(rec) {
		t.Fatal("WithProgressReporter did not store the reporter")
	}
}
