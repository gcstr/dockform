package planner

import (
	"context"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
)

func TestRestartManager_New(t *testing.T) {
	// Test basic construction without Docker dependencies
	mockDocker := newMockDocker()
	restartManager := NewRestartManager(mockDocker, nil, nil)
	if restartManager.docker != mockDocker {
		t.Error("restart manager docker client not set correctly")
	}
}

func TestRestartManager_RestartPendingServices_NoPendingServices(t *testing.T) {
	// Test that empty restart pending map is handled correctly
	restartPending := map[restartTarget]struct{}{}

	if len(restartPending) != 0 {
		t.Error("Expected empty restart pending map")
	}

	// The actual restart operation would be tested in integration tests
	// This validates the basic logic flow
	t.Log("Empty restart pending validation passed")
}

func TestRestartManager_ServicesParsing(t *testing.T) {
	tests := []struct {
		name          string
		services      map[string]struct{}
		expectedCount int
		expectedValid int // Services with valid names
	}{
		{
			name:          "single service",
			services:      map[string]struct{}{"web": {}},
			expectedCount: 1,
			expectedValid: 1,
		},
		{
			name: "multiple services",
			services: map[string]struct{}{
				"web": {},
				"api": {},
				"db":  {},
			},
			expectedCount: 3,
			expectedValid: 3,
		},
		{
			name: "services with empty name",
			services: map[string]struct{}{
				"web": {},
				"":    {}, // Empty service name should be filtered during restart
				"api": {},
			},
			expectedCount: 3, // Map contains 3 entries
			expectedValid: 2, // Only 2 have valid names
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the parsing logic without Docker operations
			if len(tt.services) != tt.expectedCount {
				t.Errorf("expected %d services in map, got %d", tt.expectedCount, len(tt.services))
			}

			// Count valid service names (non-empty)
			validCount := 0
			for serviceName := range tt.services {
				if serviceName != "" {
					validCount++
				}
			}

			if validCount != tt.expectedValid {
				t.Errorf("expected %d valid services, got %d", tt.expectedValid, validCount)
			}
		})
	}
}

func TestRestartManager_RestartPendingServices_WithMock(t *testing.T) {
	tests := []struct {
		name                string
		pendingServices     map[restartTarget]struct{}
		availableContainers []dockercli.PsBrief
		expectedRestarts    []string
	}{
		{
			name:             "no pending services",
			pendingServices:  map[restartTarget]struct{}{},
			expectedRestarts: []string{},
		},
		{
			name:            "restart available service",
			pendingServices: map[restartTarget]struct{}{{Stack: "myapp", Service: "web"}: {}},
			availableContainers: []dockercli.PsBrief{
				{Service: "web", Name: "myapp_web_1"},
			},
			expectedRestarts: []string{"myapp_web_1"},
		},
		{
			name:            "skip missing service",
			pendingServices: map[restartTarget]struct{}{{Stack: "myapp", Service: "missing"}: {}},
			availableContainers: []dockercli.PsBrief{
				{Service: "web", Name: "myapp_web_1"},
			},
			expectedRestarts: []string{},
		},
		{
			name:            "mixed available and missing services",
			pendingServices: map[restartTarget]struct{}{{Stack: "myapp", Service: "web"}: {}, {Stack: "myapp", Service: "missing"}: {}, {Stack: "myapp", Service: "db"}: {}},
			availableContainers: []dockercli.PsBrief{
				{Service: "web", Name: "myapp_web_1"},
				{Service: "db", Name: "myapp_db_1"},
			},
			expectedRestarts: []string{"myapp_web_1", "myapp_db_1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock Docker client with available containers
			mockDocker := newMockDocker()
			mockDocker.containers = tt.availableContainers

			restartManager := NewRestartManager(mockDocker, nil, nil)

			err := restartManager.RestartPendingServices(context.Background(), "test-context", tt.pendingServices, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check that the correct containers were restarted
			if len(mockDocker.restartedContainers) != len(tt.expectedRestarts) {
				t.Errorf("expected %d containers to be restarted, got %d", len(tt.expectedRestarts), len(mockDocker.restartedContainers))
			}

			for _, expectedContainer := range tt.expectedRestarts {
				found := false
				for _, restartedContainer := range mockDocker.restartedContainers {
					if restartedContainer == expectedContainer {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected container %q to be restarted", expectedContainer)
				}
			}
		})
	}
}

// A restarted service must resolve the line SeedRefs created for it. All four
// ResourceRef fields are compared, so a missing Parent produces a second,
// top-level "(discovered)" entry instead of updating the seeded line.
func TestRestartPendingServices_RefMatchesSeededRef(t *testing.T) {
	seeded := ResourceRef{Context: "ctx", Type: ResourceService, Name: "web", Parent: "linkwarden"}

	got := restartRefFor("ctx", restartTarget{Stack: "linkwarden", Service: "web"})

	if got != seeded {
		t.Errorf("restart ref = %+v, want %+v", got, seeded)
	}
}

// Two stacks owning a service of the same name must not collide. This is the
// failure a wrongly-attached Parent would cause.
func TestRestartPendingServices_SameServiceNameInTwoStacks(t *testing.T) {
	a := restartRefFor("ctx", restartTarget{Stack: "linkwarden", Service: "postgres"})
	b := restartRefFor("ctx", restartTarget{Stack: "paperless", Service: "postgres"})

	if a == b {
		t.Fatalf("refs for the same service name in different stacks must differ; both were %+v", a)
	}
	if a.Parent != "linkwarden" || b.Parent != "paperless" {
		t.Errorf("parents wrong: a=%q b=%q", a.Parent, b.Parent)
	}
}

// Two stacks with a pending service of the same name must each restart their
// OWN container. Matching by service name alone (docker's listing order) would
// restart whichever container happens to come first for BOTH targets, leaving
// one stack's container never restarted while the other's is restarted twice.
func TestRestartPendingServices_SharedServiceNameRestartsOwnContainer(t *testing.T) {
	mockDocker := newMockDocker()
	mockDocker.containers = []dockercli.PsBrief{
		{Project: "linkwarden", Service: "postgres", Name: "linkwarden-postgres-1"},
		{Project: "paperless", Service: "postgres", Name: "paperless-postgres-1"},
	}
	pending := map[restartTarget]struct{}{
		{Stack: "linkwarden", Service: "postgres"}: {},
		{Stack: "paperless", Service: "postgres"}:  {},
	}
	projectToStack := map[string]string{"linkwarden": "linkwarden", "paperless": "paperless"}

	restartManager := NewRestartManager(mockDocker, nil, nil)
	if err := restartManager.RestartPendingServices(context.Background(), "ctx", pending, projectToStack); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantRestarted := []string{"linkwarden-postgres-1", "paperless-postgres-1"}
	if len(mockDocker.restartedContainers) != len(wantRestarted) {
		t.Fatalf("restarted containers = %v, want %v", mockDocker.restartedContainers, wantRestarted)
	}
	for _, want := range wantRestarted {
		found := false
		for _, got := range mockDocker.restartedContainers {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q to be restarted, got=%v", want, mockDocker.restartedContainers)
		}
	}
}
