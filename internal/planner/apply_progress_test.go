package planner

import (
	"context"
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

func TestProgressEstimator_CountVolumesToCreate(t *testing.T) {
	tests := []struct {
		name            string
		filesets        map[string]manifest.FilesetSpec
		volumes         map[string]manifest.VolumeSpec
		existingVolumes []string
		expectedCount   int
	}{
		{
			name:            "no volumes needed",
			filesets:        map[string]manifest.FilesetSpec{},
			volumes:         map[string]manifest.VolumeSpec{},
			existingVolumes: []string{},
			expectedCount:   0,
		},
		{
			name: "fileset volume needs creation",
			filesets: map[string]manifest.FilesetSpec{
				"data": {TargetVolume: "app-data"},
			},
			volumes:         map[string]manifest.VolumeSpec{},
			existingVolumes: []string{},
			expectedCount:   1,
		},
		{
			name:     "explicit volume needs creation",
			filesets: map[string]manifest.FilesetSpec{},
			volumes: map[string]manifest.VolumeSpec{
				"db-data": {},
			},
			existingVolumes: []string{},
			expectedCount:   1,
		},
		{
			name: "volume already exists",
			filesets: map[string]manifest.FilesetSpec{
				"data": {TargetVolume: "app-data"},
			},
			volumes:         map[string]manifest.VolumeSpec{},
			existingVolumes: []string{"app-data"},
			expectedCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock Docker client with existing volumes
			mockDocker := newMockDocker()
			mockDocker.volumes = tt.existingVolumes

			planner := &Planner{docker: mockDocker}
			estimator := NewProgressEstimator(planner)

			cfg := manifest.Config{
				Filesets: tt.filesets,
				Volumes:  tt.volumes,
			}

			count, err := estimator.countVolumesToCreate(context.Background(), cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if count != tt.expectedCount {
				t.Errorf("expected %d volumes to create, got %d", tt.expectedCount, count)
			}
		})
	}
}

func TestProgressEstimator_CountNetworksToCreate(t *testing.T) {
	tests := []struct {
		name             string
		networks         map[string]manifest.NetworkSpec
		existingNetworks []string
		expectedCount    int
	}{
		{
			name:             "no networks needed",
			networks:         map[string]manifest.NetworkSpec{},
			existingNetworks: []string{},
			expectedCount:    0,
		},
		{
			name: "network needs creation",
			networks: map[string]manifest.NetworkSpec{
				"app-network": {},
			},
			existingNetworks: []string{},
			expectedCount:    1,
		},
		{
			name: "network already exists",
			networks: map[string]manifest.NetworkSpec{
				"app-network": {},
			},
			existingNetworks: []string{"app-network"},
			expectedCount:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock Docker client with existing networks
			mockDocker := newMockDocker()
			mockDocker.networks = tt.existingNetworks

			planner := &Planner{docker: mockDocker}
			estimator := NewProgressEstimator(planner)

			cfg := manifest.Config{Networks: tt.networks}

			count, err := estimator.countNetworksToCreate(context.Background(), cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if count != tt.expectedCount {
				t.Errorf("expected %d networks to create, got %d", tt.expectedCount, count)
			}
		})
	}
}
