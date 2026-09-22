package planner

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
)

type trackerRun struct {
	rec     *recordingReporter
	tracker *stackProgress
}

func newTrackerRun(project string, services map[string]dockercli.ComposeService, changed ...string) trackerRun {
	set := map[string]bool{}
	for _, s := range changed {
		set[s] = true
	}
	rec := &recordingReporter{}
	resolver := newContainerResolver(dockercli.ComposeConfigDoc{Name: project, Services: services}, project)
	return trackerRun{rec: rec, tracker: newStackProgress(rec, "ctx", "probe", set, resolver)}
}

func (r trackerRun) replay(events []dockercli.ComposeEvent) {
	for _, ev := range events {
		r.tracker.OnEvent(ev)
	}
}

func svcRef(name string) ResourceRef {
	return ResourceRef{Context: "ctx", Type: ResourceService, Name: name, Parent: "probe"}
}

var probeStackRef = ResourceRef{Context: "ctx", Type: ResourceStack, Name: "probe"}

// calls returns "kind:text" for every event recorded against ref, in order.
func (r trackerRun) calls(ref ResourceRef) []string {
	r.rec.mu.Lock()
	defer r.rec.mu.Unlock()
	var out []string
	for _, ev := range r.rec.events {
		if ev.Ref == ref {
			out = append(out, ev.Kind+":"+ev.Text)
		}
	}
	return out
}

func assertCalls(t *testing.T, run trackerRun, ref ResourceRef, want ...string) {
	t.Helper()
	if got := run.calls(ref); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: got %q, want %q", ref.Name, got, want)
	}
}

func TestStackProgress_Recreate(t *testing.T) {
	run := newTrackerRun("skqfixture", map[string]dockercli.ComposeService{"alpha": {}, "beta": {}}, "alpha", "beta")
	run.replay(composeFixture(t, "recreate.jsonl"))
	for _, svc := range []string{"alpha", "beta"} {
		assertCalls(t, run, svcRef(svc), "start:recreating", "detail:starting…", "finish:recreated")
	}
}

func TestStackProgress_Create(t *testing.T) {
	run := newTrackerRun("skqfixture", map[string]dockercli.ComposeService{"alpha": {}, "beta": {}}, "alpha", "beta")
	run.replay(composeFixture(t, "create.jsonl"))
	assertCalls(t, run, svcRef("alpha"), "start:creating", "detail:starting…", "finish:created")
}

func TestStackProgress_StartsAStoppedContainer(t *testing.T) {
	run := newTrackerRun("skqstop", map[string]dockercli.ComposeService{"web": {}}, "web")
	run.replay(composeFixture(t, "start_stopped.jsonl"))
	assertCalls(t, run, svcRef("web"), "start:starting", "finish:started")
}

func TestStackProgress_ContainerNameOverride(t *testing.T) {
	run := newTrackerRun("skqcn", map[string]dockercli.ComposeService{"gamma": {ContainerName: "totally-custom-name"}}, "gamma")
	run.replay(composeFixture(t, "container_name_override.jsonl"))
	assertCalls(t, run, svcRef("gamma"), "start:creating", "detail:starting…", "finish:created")
}

// db and bystander are not changed: they get no lines, but db's healthcheck
// wait is exactly what makes app slow, so it shows on the stack line.
func TestStackProgress_DependencyWaitGoesToTheStackLine(t *testing.T) {
	run := newTrackerRun("skqhc", map[string]dockercli.ComposeService{"db": {}, "app": {}, "bystander": {}}, "app")
	run.replay(composeFixture(t, "depends_on_healthy.jsonl"))
	assertCalls(t, run, probeStackRef, "detail:waiting: db healthy", "detail:")
	assertCalls(t, run, svcRef("app"), "start:creating", "detail:starting…", "finish:created")
	assertCalls(t, run, svcRef("db"))
	assertCalls(t, run, svcRef("bystander"))
}

func TestStackProgress_UnchangedServicesGetNoLines(t *testing.T) {
	run := newTrackerRun("skqhc", map[string]dockercli.ComposeService{"db": {}, "app": {}, "bystander": {}}, "app")
	run.replay(composeFixture(t, "partial_change_with_unchanged_deps.jsonl"))
	assertCalls(t, run, svcRef("db"))
	assertCalls(t, run, svcRef("bystander"))
	assertCalls(t, run, svcRef("app"), "start:recreating", "detail:starting…", "finish:recreated")
	assertCalls(t, run, probeStackRef, "detail:waiting: db healthy", "detail:")
}

// An SSH retry replays compose from the start. A finished line ignores it.
func TestStackProgress_ReplayedEventsAreHarmless(t *testing.T) {
	run := newTrackerRun("skqfixture", map[string]dockercli.ComposeService{"alpha": {}, "beta": {}}, "alpha", "beta")
	events := composeFixture(t, "recreate.jsonl")
	run.replay(events)
	run.replay(events)
	assertCalls(t, run, svcRef("alpha"), "start:recreating", "detail:starting…", "finish:recreated")
}

func TestStackProgress_PullReportsIncreasingPercentages(t *testing.T) {
	run := newTrackerRun("skqmulti", map[string]dockercli.ComposeService{"db": {Image: "postgres:16.4-bookworm"}}, "db")
	run.replay(composeFixture(t, "pull_multilayer.jsonl"))
	calls := run.calls(svcRef("db"))
	if len(calls) == 0 || calls[0] != "start:pulling" {
		t.Fatalf("first call = %v, want start:pulling", calls)
	}
	assertClimbsTo100(t, run.percentages(svcRef("db")))
}

// percentages returns every "progress:N" recorded against ref, in order.
func (r trackerRun) percentages(ref ResourceRef) []int {
	var out []int
	for _, c := range r.calls(ref) {
		if pct, ok := strings.CutPrefix(c, "progress:"); ok {
			n, _ := strconv.Atoi(pct)
			out = append(out, n)
		}
	}
	return out
}

// Options.StderrLine does not disable exec.go's SSH retry, so attempt 2 replays
// the same layer ids into the same tracker. The accumulator folds with max and
// suppresses any reading at or below the last one, so without a reset the line
// sits frozen at attempt 1's high-water mark until the re-download passes it.
func TestStackProgress_ARetriedPullRestartsThePercentage(t *testing.T) {
	run := newTrackerRun("skqmulti", map[string]dockercli.ComposeService{"db": {Image: "postgres:16.4-bookworm"}}, "db")
	events := composeFixture(t, "pull_multilayer.jsonl")
	run.replay(events[:len(events)*3/4]) // attempt 1: an SSH drop mid-pull
	first := run.percentages(svcRef("db"))
	if len(first) == 0 || first[len(first)-1] < 50 {
		t.Fatalf("attempt 1 must reach a high-water mark worth being stuck at: %v", first)
	}
	run.replay(events) // attempt 2: compose starts over
	all := run.percentages(svcRef("db"))
	second := all[len(first):]
	if len(second) == 0 {
		t.Fatal("attempt 2 reported no percentage at all")
	}
	if high := first[len(first)-1]; second[0] >= high {
		t.Fatalf("attempt 2 began at %d%%, still stuck at attempt 1's %d%%: %v", second[0], high, all)
	}
	assertClimbsTo100(t, second)
}

// A wait the dropped attempt never resolved would otherwise sit on the stack
// line for the rest of the run: attempt 2 need not wait on that dependency
// again, so no Healthy ever arrives to clear it.
func TestStackProgress_AReplayClearsAnUnresolvedWait(t *testing.T) {
	run := newTrackerRun("skqhc", map[string]dockercli.ComposeService{"db": {}, "app": {}, "bystander": {}}, "app")
	events := composeFixture(t, "depends_on_healthy.jsonl")
	cut := 0
	for i, ev := range events {
		if ev.Text == "Waiting" {
			cut = i + 1
			break
		}
	}
	run.replay(events[:cut]) // attempt 1 dies while db is still starting up
	run.replay(events[:1])   // attempt 2 opens with the same first event
	calls := run.calls(probeStackRef)
	if len(calls) == 0 || calls[len(calls)-1] != "detail:" {
		t.Fatalf("stack line still shows the stale wait: %v", calls)
	}
}

func waitEvent(container, text string) dockercli.ComposeEvent {
	status := "Working"
	if text == "Healthy" {
		status = "Done"
	}
	return dockercli.ComposeEvent{Kind: dockercli.EventContainer, Name: container, Status: status, Text: text}
}

func TestStackProgress_SeveralWaitsAtOnce(t *testing.T) {
	run := newTrackerRun("x", map[string]dockercli.ComposeService{"db": {}, "redis": {}, "app": {}}, "app")
	run.replay([]dockercli.ComposeEvent{
		waitEvent("x-db-1", "Waiting"),
		waitEvent("x-redis-1", "Waiting"),
		waitEvent("x-db-1", "Healthy"),
		waitEvent("x-redis-1", "Healthy"),
	})
	assertCalls(t, run, probeStackRef,
		"detail:waiting: db healthy",
		"detail:waiting: db, redis healthy",
		"detail:waiting: redis healthy",
		"detail:")
}

// A wait is never silently dropped: an unresolvable container shows by name.
func TestStackProgress_UnresolvableWaitShowsTheContainerName(t *testing.T) {
	run := newTrackerRun("x", map[string]dockercli.ComposeService{"app": {}}, "app")
	run.replay([]dockercli.ComposeEvent{waitEvent("mystery", "Waiting")})
	assertCalls(t, run, probeStackRef, "detail:waiting: mystery healthy")
}
