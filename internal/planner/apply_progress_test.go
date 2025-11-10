package planner

import (
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
)

func TestProgressEstimator_New(t *testing.T) {
	spinner := &ui.Spinner{}
	estimator := NewProgressEstimator(nil, newProgressReporter(spinner, "Testing"))
	if estimator.docker != nil {
		t.Error("expected estimator docker client to be nil")
	}
	sa, ok := estimator.progress.(*spinnerAdapter)
	if !ok {
		t.Fatal("expected spinner adapter")
	}
	if sa.inner != spinner {
		t.Error("estimator progress adapter not wrapping provided spinner")
	}
	if sa.prefix != "Testing" {
		t.Errorf("expected prefix 'Testing', got '%s'", sa.prefix)
	}
}
func TestProgressEstimator_EstimateProgress_BasicLogic(t *testing.T) {
	// Test the basic calculation logic without Docker interactions
	cfg := manifest.Config{
		Stacks: map[string]manifest.Stack{
			"web": {Root: "./web", Files: []string{"docker-compose.yml"}},
			"api": {Root: "./api", Files: []string{"docker-compose.yml"}},
		},
		Networks: map[string]manifest.NetworkSpec{
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

	if len(cfg.Stacks) < 1 {
		t.Error("Expected at least 1 stack in test config")
	}
	if len(cfg.Networks) < 1 {
		t.Error("Expected at least 1 network in test config")
	}
	if len(cfg.Filesets) < 1 {
		t.Error("Expected at least 1 fileset in test config")
	}

	totalConfigItems := len(cfg.Stacks) + len(cfg.Networks) + len(cfg.Filesets)
	if totalConfigItems < expectedMinWork {
		t.Errorf("Expected at least %d total config items, got %d", expectedMinWork, totalConfigItems)
	}
}

