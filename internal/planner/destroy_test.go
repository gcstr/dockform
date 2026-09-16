package planner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

// mockDockerListCounter wraps mockDockerClient to count ListComposeContainersAll calls
type mockDockerListCounter struct {
	*mockDockerClient
	listCallCount int
}

func (m *mockDockerListCounter) ListComposeContainersAll(ctx context.Context) ([]dockercli.PsBrief, error) {
	m.listCallCount++
	return m.mockDockerClient.ListComposeContainersAll(ctx)
}

func TestDestroy_ListsContainersOnce(t *testing.T) {
	// Setup mock docker with multiple containers across different apps/services
	baseMock := newMockDocker()
	baseMock.containers = []dockercli.PsBrief{
		{Project: "app1", Service: "web", Name: "app1-web-1"},
		{Project: "app1", Service: "db", Name: "app1-db-1"},
		{Project: "app2", Service: "api", Name: "app2-api-1"},
		{Project: "app2", Service: "cache", Name: "app2-cache-1"},
	}
	baseMock.volumes = []string{"vol1", "vol2"}
	baseMock.networks = []string{"net1"}

	mockCounter := &mockDockerListCounter{mockDockerClient: baseMock}

	cfg := manifest.Config{
		Identifier: "test",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{},
	}

	planner := NewWithDocker(mockCounter)
	ctx := context.Background()

	// Execute destroy
	if err := planner.Destroy(ctx, cfg); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	// Verify ListComposeContainersAll was called exactly once during execution.
	// Destroy directly removes containers per-daemon without building a plan first.
	if mockCounter.listCallCount != 1 {
		t.Errorf("Expected ListComposeContainersAll to be called 1 time, got %d", mockCounter.listCallCount)
	}

	// Verify all containers were removed
	if len(mockCounter.removedContainers) != 4 {
		t.Errorf("Expected 4 containers to be removed, got %d: %v", len(mockCounter.removedContainers), mockCounter.removedContainers)
	}

	// Verify all networks were removed
	if len(mockCounter.removedNetworks) != 1 {
		t.Errorf("Expected 1 network to be removed, got %d", len(mockCounter.removedNetworks))
	}

	// Verify all volumes were removed
	if len(mockCounter.removedVolumes) != 2 {
		t.Errorf("Expected 2 volumes to be removed, got %d", len(mockCounter.removedVolumes))
	}
}

func TestDestroy_OptimizedContainerLookup(t *testing.T) {
	// Test that the optimized lookup correctly handles multiple containers per service
	baseMock := newMockDocker()
	baseMock.containers = []dockercli.PsBrief{
		{Project: "myapp", Service: "web", Name: "myapp-web-1"},
		{Project: "myapp", Service: "web", Name: "myapp-web-2"}, // scaled service
		{Project: "myapp", Service: "db", Name: "myapp-db-1"},
		{Project: "", Service: "", Name: "orphan-1"}, // orphan without project
	}

	mockCounter := &mockDockerListCounter{mockDockerClient: baseMock}

	cfg := manifest.Config{
		Identifier: "test",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
	}

	planner := NewWithDocker(mockCounter)
	ctx := context.Background()

	if err := planner.Destroy(ctx, cfg); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	// All 4 containers should be removed
	if len(mockCounter.removedContainers) != 4 {
		t.Errorf("Expected 4 containers removed, got %d: %v", len(mockCounter.removedContainers), mockCounter.removedContainers)
	}

	// Verify both scaled web containers were removed
	webCount := 0
	for _, name := range mockCounter.removedContainers {
		if name == "myapp-web-1" || name == "myapp-web-2" {
			webCount++
		}
	}
	if webCount != 2 {
		t.Errorf("Expected 2 web containers removed, got %d", webCount)
	}
}

// TestDestroy_ScopedToStack verifies that when the config is targeted (e.g. via
// --stack), destroy only removes the targeted stack's services and its own
// fileset volumes, leaving other stacks and context-level shared
// networks/volumes untouched. Regression test for GH #55.
func TestDestroy_ScopedToStack(t *testing.T) {
	baseMock := newMockDocker()
	baseMock.containers = []dockercli.PsBrief{
		{Project: "nginx", Service: "nginx", Name: "nginx-nginx-1"},
		{Project: "traefik", Service: "traefik", Name: "traefik-traefik-1"},
	}
	baseMock.networks = []string{"proxy", "traefik"}
	baseMock.volumes = []string{"nginx-config", "traefik-config", "traefik-logs"}

	mockCounter := &mockDockerListCounter{mockDockerClient: baseMock}

	// Config targeted to services/nginx only.
	cfg := manifest.Config{
		Identifier: "test",
		Targeted:   true,
		Contexts: map[string]manifest.ContextConfig{
			"services": {},
		},
		Stacks: map[string]manifest.Stack{
			"services/nginx": {Context: "services", Root: filepath.Join(t.TempDir(), "nginx")},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{
			"nginx-config": {TargetVolume: "nginx-config", Context: "services", Stack: "nginx"},
		},
	}

	planner := NewWithDocker(mockCounter)
	ctx := context.Background()

	if err := planner.Destroy(ctx, cfg); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	// Only the nginx service container should be removed.
	if got := mockCounter.removedContainers; len(got) != 1 || got[0] != "nginx-nginx-1" {
		t.Errorf("Expected only nginx-nginx-1 removed, got %v", got)
	}

	// Context-level shared networks must NOT be removed under a scoped destroy.
	if got := mockCounter.removedNetworks; len(got) != 0 {
		t.Errorf("Expected no networks removed under scoped destroy, got %v", got)
	}

	// Only the targeted stack's fileset volume should be removed; shared/other
	// volumes must be left alone.
	if got := mockCounter.removedVolumes; len(got) != 1 || got[0] != "nginx-config" {
		t.Errorf("Expected only nginx-config volume removed, got %v", got)
	}
}

// A targeted stack whose compose file sets name: (or COMPOSE_PROJECT_NAME) runs
// under that project, so scoped destroy must match it rather than the stack name.
func TestDestroy_ScopedToStack_UsesComposeProjectName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "web")
	mock := newMockDocker()
	mock.composeProjectNames = map[string]string{root: "custom"}
	mock.containers = []dockercli.PsBrief{
		{Project: "custom", Service: "web", Name: "custom-web-1"},
		{Project: "web", Service: "web", Name: "web-web-1"}, // another stack that happens to be named like the directory
	}
	cfg := manifest.Config{
		Identifier: "test",
		Targeted:   true,
		Contexts:   map[string]manifest.ContextConfig{"services": {}},
		Stacks:     map[string]manifest.Stack{"services/web": {Context: "services", Root: root}},
	}
	p := NewWithDocker(mock)

	plan, err := p.BuildDestroyPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildDestroyPlan failed: %v", err)
	}
	if _, ok := plan.Resources.Stacks["services/custom"]; !ok || len(plan.Resources.Stacks) != 1 {
		t.Errorf("destroy plan stacks = %v, want only services/custom", plan.Resources.Stacks)
	}

	if err := p.Destroy(context.Background(), cfg); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}
	if got := mock.removedContainers; len(got) != 1 || got[0] != "custom-web-1" {
		t.Errorf("removed containers = %v, want [custom-web-1]", got)
	}
}

// If a targeted stack's project cannot be resolved, destroy fails loudly instead
// of guessing and silently removing nothing (or the wrong thing).
func TestDestroy_ScopedToStack_UnresolvedProjectFails(t *testing.T) {
	mock := newMockDocker()
	mock.composeConfigFullError = errors.New("compose config failed")
	mock.containers = []dockercli.PsBrief{{Project: "web", Service: "web", Name: "web-web-1"}}
	cfg := manifest.Config{
		Identifier: "test",
		Targeted:   true,
		Contexts:   map[string]manifest.ContextConfig{"services": {}},
		Stacks:     map[string]manifest.Stack{"services/web": {Context: "services", Root: filepath.Join(t.TempDir(), "web")}},
	}
	p := NewWithDocker(mock)

	if _, err := p.BuildDestroyPlan(context.Background(), cfg); err == nil {
		t.Error("BuildDestroyPlan: expected an error for an unresolvable project")
	}
	if err := p.Destroy(context.Background(), cfg); err == nil {
		t.Error("Destroy: expected an error for an unresolvable project")
	}
	if got := mock.removedContainers; len(got) != 0 {
		t.Errorf("removed containers = %v, want none", got)
	}
}

// buildDestroyPlanForContext must key its own ResourcePlan.Stacks bare (just
// the project name): destroy renders from Plan.ByContext now, which already
// nests each context under its own header (see renderPlanByContext), so a
// "context/project" key there would print the context twice — the same bug
// Finding 1 fixed for filesets.
//
// The context prefix still has to exist somewhere, though: BuildDestroyPlan's
// aggregated Resources merges every context's plan into one flat map, and two
// hosts routinely run same-named projects. mergeResourcePlan is the one that
// must add it back, mirroring aggregateContextPlan (used by the regular plan
// path) — dropping the prefix outright would let host-a's and host-b's "app1"
// collide into a single aggregate entry, undercounting the destroy plan by a
// whole stack.
func TestBuildDestroyPlan_StacksKeyedBarePerContext_PrefixedInAggregate(t *testing.T) {
	mock := newMockDocker()
	mock.containers = []dockercli.PsBrief{
		{Project: "app1", Service: "web", Name: "app1-web-1"},
	}
	cfg := manifest.Config{
		Identifier: "test",
		Contexts: map[string]manifest.ContextConfig{
			"host-a": {},
			"host-b": {},
		},
	}
	p := NewWithDocker(mock)

	plan, err := p.BuildDestroyPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildDestroyPlan failed: %v", err)
	}

	for _, ctxName := range []string{"host-a", "host-b"} {
		cp, ok := plan.ByContext[ctxName]
		if !ok {
			t.Fatalf("missing ByContext[%q]", ctxName)
		}
		if _, ok := cp.Resources.Stacks["app1"]; !ok {
			t.Errorf("ByContext[%q].Resources.Stacks = %v, want bare key %q", ctxName, cp.Resources.Stacks, "app1")
		}
		if _, ok := cp.Resources.Stacks[ctxName+"/app1"]; ok {
			t.Errorf("ByContext[%q].Resources.Stacks must not repeat its own context prefix; got %v", ctxName, cp.Resources.Stacks)
		}
	}

	// Aggregate must disambiguate the two same-named projects by context,
	// rather than collapsing them into one "app1" entry.
	if _, ok := plan.Resources.Stacks["app1"]; ok {
		t.Errorf("aggregate Resources.Stacks must not use the bare key when contexts collide; got %v", plan.Resources.Stacks)
	}
	for _, key := range []string{"host-a/app1", "host-b/app1"} {
		svcs, ok := plan.Resources.Stacks[key]
		if !ok {
			t.Fatalf("aggregate Resources.Stacks missing %q; got %v", key, plan.Resources.Stacks)
		}
		if len(svcs) != 1 {
			t.Errorf("aggregate Resources.Stacks[%q] = %v, want exactly 1 service (its own host's), not merged with the other host's", key, svcs)
		}
	}
	if len(plan.Resources.Stacks) != 2 {
		t.Errorf("aggregate Resources.Stacks has %d entries, want 2 (one per context); got %v", len(plan.Resources.Stacks), plan.Resources.Stacks)
	}
}
