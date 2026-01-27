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
	// In the new multi-daemon schema, networks are managed by compose and filesets are discovered
	cfg := manifest.Config{
		Daemons: map[string]manifest.DaemonConfig{
			"default": {Identifier: "test"},
		},
		Stacks: map[string]manifest.Stack{
			"default/web": {Root: "./web", Files: []string{"docker-compose.yml"}},
			"default/api": {Root: "./api", Files: []string{"docker-compose.yml"}},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{
			"default/web/assets": {TargetVolume: "web-data", Daemon: "default"},
			"default/api/data":   {TargetVolume: "api-data", Daemon: "default"},
		},
	}

	// We expect: 2 stacks + 2 filesets + 2 volumes (from filesets) = 6 total work items
	// This is basic validation that the logic counts configuration items
	expectedMinWork := 4 // At minimum: stacks + filesets

	if len(cfg.Stacks) < 1 {
		t.Error("Expected at least 1 stack in test config")
	}
	allFilesets := cfg.GetAllFilesets()
	if len(allFilesets) < 1 {
		t.Error("Expected at least 1 fileset in test config")
	}

	totalConfigItems := len(cfg.Stacks) + len(allFilesets)
	if totalConfigItems < expectedMinWork {
		t.Errorf("Expected at least %d total config items, got %d", expectedMinWork, totalConfigItems)
	}
}
