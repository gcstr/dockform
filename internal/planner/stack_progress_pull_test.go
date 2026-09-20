package planner

import (
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
)

func pullReports(events []dockercli.ComposeEvent) []int {
	p := newPullProgress()
	var got []int
	for _, ev := range events {
		if ev.Kind != dockercli.EventLayer {
			continue
		}
		if pct, ok := p.observe(ev); ok {
			got = append(got, pct)
		}
	}
	return got
}

func assertClimbsTo100(t *testing.T, got []int) {
	t.Helper()
	if len(got) == 0 {
		t.Fatal("no percentage was ever reported")
	}
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("percentage did not strictly increase at %d: %v", i, got)
		}
	}
	if last := got[len(got)-1]; last != 100 {
		t.Fatalf("last percentage = %d, want 100: %v", last, got)
	}
}

func TestPullProgress_MultilayerNeverDecreases(t *testing.T) {
	got := pullReports(composeFixture(t, "pull_multilayer.jsonl"))
	assertClimbsTo100(t, got)
	// The naive algorithm's first reading is 100%, from a tiny layer finishing
	// first. This one must not start near the end.
	if got[0] >= 50 {
		t.Fatalf("first reading = %d%%, want well below 50%%", got[0])
	}
}

// A layer already on disk emits only "Already exists", with no total. If it
// counted as announced, the percentage would never appear.
func TestPullProgress_SharedBaseLayerStillReports(t *testing.T) {
	assertClimbsTo100(t, pullReports(composeFixture(t, "pull_shared_base_layer.jsonl")))
}

func TestPullProgress_SingleLayer(t *testing.T) {
	assertClimbsTo100(t, pullReports(composeFixture(t, "pull_with_layers.jsonl")))
}

func layer(id, text string, current, total int64) dockercli.ComposeEvent {
	return dockercli.ComposeEvent{Kind: dockercli.EventLayer, Name: id, Image: "img:1", Status: "Working", Text: text, Current: current, Total: total}
}

func TestPullProgress_NothingUntilEveryAnnouncedLayerHasATotal(t *testing.T) {
	p := newPullProgress()
	for _, ev := range []dockercli.ComposeEvent{
		layer("a", "Pulling fs layer", 0, 0),
		layer("b", "Pulling fs layer", 0, 0),
		layer("a", "Downloading", 50, 100),
	} {
		if pct, ok := p.observe(ev); ok {
			t.Fatalf("reported %d%% while layer b had no total", pct)
		}
	}
	if pct, ok := p.observe(layer("b", "Downloading", 0, 100)); !ok || pct != 12 {
		t.Fatalf("got %d, %v; want 12, true ((50+0)/(2*200))", pct, ok)
	}
}

func TestPullProgress_AlreadyExistsLayerIsNotAnnounced(t *testing.T) {
	p := newPullProgress()
	p.observe(layer("cached", "Already exists", 0, 0))
	p.observe(layer("new", "Pulling fs layer", 0, 0))
	pct, ok := p.observe(layer("new", "Pull complete", 0, 0))
	if ok {
		t.Fatalf("reported %d%% before the new layer had a total", pct)
	}
	p.observe(layer("new", "Downloading", 100, 100))
	if pct, ok := p.observe(layer("new", "Pull complete", 0, 0)); !ok || pct != 100 {
		t.Fatalf("got %d, %v; want 100, true", pct, ok)
	}
}

func TestPullProgress_OnlyReportsAChange(t *testing.T) {
	p := newPullProgress()
	p.observe(layer("a", "Pulling fs layer", 0, 0))
	if _, ok := p.observe(layer("a", "Downloading", 10, 100)); !ok {
		t.Fatal("expected a first report")
	}
	if pct, ok := p.observe(layer("a", "Downloading", 10, 100)); ok {
		t.Fatalf("repeated %d%% with no change", pct)
	}
}
