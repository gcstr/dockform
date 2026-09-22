package planner

import (
	"context"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

func applyProbeStack(t *testing.T, docker *mockDockerClient) *recordingReporter {
	t.Helper()
	rec := &recordingReporter{}
	execCtx := &ContextExecutionContext{Stacks: map[string]*StackExecutionData{
		"probe": {
			Services: []ServiceInfo{
				{Name: "alpha", State: ServiceDrifted},
				{Name: "beta", State: ServiceDrifted},
			},
			NeedsApply: true,
		},
	}}
	stacks := map[string]manifest.Stack{"probe": {Root: "/stacks/skqfixture"}}
	if err := New().applyStackChangesForContext(context.Background(), manifest.Config{}, "ctx", stacks, "",
		docker, map[restartTarget]struct{}{}, rec, execCtx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	return rec
}

func probeDocker() *mockDockerClient {
	docker := newMockDocker()
	docker.composeConfigDocs = map[string]dockercli.ComposeConfigDoc{
		"/stacks/skqfixture": {Name: "skqfixture", Services: map[string]dockercli.ComposeService{
			"alpha": {Image: "alpine:3.22"},
			"beta":  {Image: "alpine:3.22"},
		}},
	}
	return docker
}

func TestApply_ReportsPerServiceProgress(t *testing.T) {
	docker := probeDocker()
	docker.progressEvents = composeFixture(t, "recreate.jsonl")
	run := trackerRun{rec: applyProbeStack(t, docker)}

	for _, svc := range []string{"alpha", "beta"} {
		assertCalls(t, run, svcRef(svc), "start:recreating", "detail:starting…", "finish:recreated")
	}
	// apply.go still owns the stack line's lifecycle.
	assertCalls(t, run, probeStackRef, "start:starting", "finish:started")
}

// No events — an unsupported compose, or a stream that never arrives — must
// leave today's stack-level behaviour exactly as it was.
func TestApply_NoProgressEventsKeepsStackLevelBehaviour(t *testing.T) {
	run := trackerRun{rec: applyProbeStack(t, probeDocker())}
	assertCalls(t, run, svcRef("alpha"))
	assertCalls(t, run, svcRef("beta"))
	assertCalls(t, run, probeStackRef, "start:starting", "finish:started")
}

// A config that cannot be read must not fail the apply: progress is advisory.
func TestApply_UnreadableConfigStillApplies(t *testing.T) {
	docker := probeDocker()
	docker.composeConfigDocs = nil
	docker.composeConfigFullError = context.DeadlineExceeded
	docker.progressEvents = composeFixture(t, "recreate.jsonl")
	run := trackerRun{rec: applyProbeStack(t, docker)}
	assertCalls(t, run, svcRef("alpha"))
	assertCalls(t, run, probeStackRef, "start:starting", "finish:started")
}
