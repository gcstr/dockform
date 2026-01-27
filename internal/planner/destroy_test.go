package planner

import (
	"context"
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
		Daemons: map[string]manifest.DaemonConfig{
			"default": {Identifier: "test"},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{},
	}

	planner := NewWithDocker(mockCounter)
	ctx := context.Background()

	// Execute destroy
	if err := planner.Destroy(ctx, cfg); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	// Verify ListComposeContainersAll was called exactly once during execution
	// (Note: BuildDestroyPlan also calls it once, so total should be 2: one for plan, one for execute)
	// Actually, looking at the code, Destroy calls BuildDestroyPlan internally, then uses the optimized loop
	// So we expect: 1 call in BuildDestroyPlan + 1 call in Destroy's optimized container removal = 2 total
	if mockCounter.listCallCount != 2 {
		t.Errorf("Expected ListComposeContainersAll to be called 2 times (once in plan, once in destroy), got %d", mockCounter.listCallCount)
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
		Daemons: map[string]manifest.DaemonConfig{
			"default": {Identifier: "test"},
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
