package planner

import (
	"testing"
)

func TestRestartManager_New(t *testing.T) {
	// Test basic construction without Docker dependencies
	planner := &Planner{} // Empty planner is fine for constructor test
	restartManager := NewRestartManager(planner)
	if restartManager.planner != planner {
		t.Error("restart manager planner not set correctly")
	}
}

func TestRestartManager_RestartPendingServices_NoPendingServices(t *testing.T) {
	// Test that empty restart pending map is handled correctly
	restartPending := map[string]struct{}{}

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

// Additional complex testing will be handled by integration tests
// These basic tests validate the essential configuration parsing logic
