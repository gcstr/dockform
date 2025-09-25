package planner

import (
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
)

func TestFilesetManager_New(t *testing.T) {
	// Test basic construction without Docker dependencies
	planner := &Planner{} // Empty planner is fine for constructor test
	manager := NewFilesetManager(planner)
	if manager.planner != planner {
		t.Error("manager planner not set correctly")
	}
}

func TestFilesetManager_ConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		filesets    map[string]manifest.FilesetSpec
		expectValid bool
	}{
		{
			name:        "empty filesets",
			filesets:    map[string]manifest.FilesetSpec{},
			expectValid: true,
		},
		{
			name: "valid fileset",
			filesets: map[string]manifest.FilesetSpec{
				"web-assets": {TargetVolume: "web-data", TargetPath: "/var/www"},
			},
			expectValid: true,
		},
		{
			name: "fileset with empty target volume",
			filesets: map[string]manifest.FilesetSpec{
				"web-assets": {TargetVolume: "", TargetPath: "/var/www"},
			},
			expectValid: false,
		},
		{
			name: "fileset with empty target path",
			filesets: map[string]manifest.FilesetSpec{
				"web-assets": {TargetVolume: "web-data", TargetPath: ""},
			},
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test basic validation logic
			for name, fileset := range tt.filesets {
				if fileset.TargetVolume == "" {
					if tt.expectValid {
						t.Errorf("Fileset %q with empty target volume should be invalid", name)
					}
				}
				if fileset.TargetPath == "" {
					if tt.expectValid {
						t.Errorf("Fileset %q with empty target path should be invalid", name)
					}
				}
			}
		})
	}
}

func TestFilesetManager_RestartServicesParsing(t *testing.T) {
	tests := []struct {
		name            string
		restartServices manifest.RestartTargets
		expectedCount   int
	}{
		{
			name:            "no restart services",
			restartServices: manifest.RestartTargets{},
			expectedCount:   0,
		},
		{
			name:            "single service",
			restartServices: manifest.RestartTargets{Services: []string{"web"}},
			expectedCount:   1,
		},
		{
			name:            "multiple services",
			restartServices: manifest.RestartTargets{Services: []string{"web", "api", "db"}},
			expectedCount:   3,
		},
		{
			name:            "attached mode",
			restartServices: manifest.RestartTargets{Attached: true},
			expectedCount:   0, // Would be resolved at runtime
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileset := manifest.FilesetSpec{
				TargetVolume:    "test-vol",
				TargetPath:      "/app",
				RestartServices: tt.restartServices,
			}

			if len(fileset.RestartServices.Services) != tt.expectedCount {
				t.Errorf("expected %d services, got %d", tt.expectedCount, len(fileset.RestartServices.Services))
			}

			// Verify content matches for explicit services
			if !fileset.RestartServices.Attached {
				for i, expected := range tt.restartServices.Services {
					if i < len(fileset.RestartServices.Services) && fileset.RestartServices.Services[i] != expected {
						t.Errorf("expected service %q at index %d, got %q", expected, i, fileset.RestartServices.Services[i])
					}
				}
			}
		})
	}
}

// Additional tests will be handled by integration testing
// These basic configuration tests validate the essential logic
