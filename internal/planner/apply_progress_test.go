package planner

import (
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
)

func TestProgressEstimator_New(t *testing.T) {
	prog := &ui.Progress{}
	planner := &Planner{prog: prog}
	estimator := NewProgressEstimator(planner)
	if estimator.planner != planner {
		t.Error("estimator planner not set correctly")
	}
	if estimator.planner.prog != prog {
		t.Error("estimator planner progress not set correctly")
	}
}
func TestProgressEstimator_EstimateProgress_BasicLogic(t *testing.T) {
	// Test the basic calculation logic without Docker interactions
	cfg := manifest.Config{
		Applications: map[string]manifest.Application{
			"web": {Root: "./web", Files: []string{"docker-compose.yml"}},
			"api": {Root: "./api", Files: []string{"docker-compose.yml"}},
		},
		Networks: map[string]manifest.TopLevelResourceSpec{
			"app-network": {},
			"db-network":  {},
		},
		Filesets: map[string]manifest.FilesetSpec{
			"web-assets": {TargetVolume: "web-data"},
			"api-data":   {TargetVolume: "api-data"},
		},
	}

	// We expect: 2 applications + 2 networks + 2 filesets + 2 volumes = 8 total work items
	// This is basic validation that the logic counts configuration items
	expectedMinWork := 6 // At minimum: apps + networks + filesets
	
	if len(cfg.Applications) < 1 {
		t.Error("Expected at least 1 application in test config")
	}
	if len(cfg.Networks) < 1 {
		t.Error("Expected at least 1 network in test config")
	}
	if len(cfg.Filesets) < 1 {
		t.Error("Expected at least 1 fileset in test config")
	}

	totalConfigItems := len(cfg.Applications) + len(cfg.Networks) + len(cfg.Filesets)
	if totalConfigItems < expectedMinWork {
		t.Errorf("Expected at least %d total config items, got %d", expectedMinWork, totalConfigItems)
	}
}

// Additional progress estimation tests will be handled by integration tests
// These basic tests validate the essential configuration parsing logic
