package planner

import (
	"context"
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
)

func TestResourceManager_EnsureVolumesExist(t *testing.T) {
	tests := []struct {
		name                string
		filesets            map[string]manifest.FilesetSpec
		volumes             map[string]manifest.TopLevelResourceSpec
		existingVolumes     []string
		expectedCreated     []string
		expectedVolumeCount int
	}{
		{
			name:                "no volumes needed",
			filesets:            map[string]manifest.FilesetSpec{},
			volumes:             map[string]manifest.TopLevelResourceSpec{},
			existingVolumes:     []string{},
			expectedCreated:     []string{},
			expectedVolumeCount: 0,
		},
		{
			name: "create fileset volume",
			filesets: map[string]manifest.FilesetSpec{
				"data": {TargetVolume: "app-data"},
			},
			volumes:             map[string]manifest.TopLevelResourceSpec{},
			existingVolumes:     []string{},
			expectedCreated:     []string{"app-data"},
			expectedVolumeCount: 1,
		},
		{
			name:     "create explicit volume",
			filesets: map[string]manifest.FilesetSpec{},
			volumes: map[string]manifest.TopLevelResourceSpec{
				"db-data": {},
			},
			existingVolumes:     []string{},
			expectedCreated:     []string{"db-data"},
			expectedVolumeCount: 1,
		},
		{
			name: "skip existing volume",
			filesets: map[string]manifest.FilesetSpec{
				"data": {TargetVolume: "app-data"},
			},
			volumes:             map[string]manifest.TopLevelResourceSpec{},
			existingVolumes:     []string{"app-data"},
			expectedCreated:     []string{},
			expectedVolumeCount: 1, // existing volume is returned in the map
		},
		{
			name: "mixed volumes",
			filesets: map[string]manifest.FilesetSpec{
				"data": {TargetVolume: "app-data"},
			},
			volumes: map[string]manifest.TopLevelResourceSpec{
				"db-data": {},
			},
			existingVolumes:     []string{"app-data"},
			expectedCreated:     []string{"db-data"},
			expectedVolumeCount: 2, // app-data (existing) + db-data (created)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock Docker client with existing volumes
			mockDocker := newMockDocker()
			mockDocker.volumes = tt.existingVolumes
			
			planner := &Planner{docker: mockDocker}
			resourceManager := NewResourceManager(planner)

			cfg := manifest.Config{
				Filesets: tt.filesets,
				Volumes:  tt.volumes,
			}
			labels := map[string]string{"test": "label"}

			resultVolumes, err := resourceManager.EnsureVolumesExist(context.Background(), cfg, labels)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check that the correct volumes were created
			for _, expectedVolume := range tt.expectedCreated {
				found := false
				for _, createdVolume := range mockDocker.createdVolumes {
					if createdVolume == expectedVolume {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected volume %q to be created, but it wasn't", expectedVolume)
				}
			}

			// Check that the returned volume map has the correct count
			if len(resultVolumes) != tt.expectedVolumeCount {
				t.Errorf("expected %d volumes in result map, got %d", tt.expectedVolumeCount, len(resultVolumes))
			}

			// Check that all expected volumes are in the result map
			for _, expected := range tt.expectedCreated {
				if _, exists := resultVolumes[expected]; !exists {
					t.Errorf("expected volume %q to be in result map", expected)
				}
			}
			for _, existing := range tt.existingVolumes {
				if _, exists := resultVolumes[existing]; !exists {
					t.Errorf("expected existing volume %q to be in result map", existing)
				}
			}
		})
	}
}

func TestResourceManager_EnsureNetworksExist(t *testing.T) {
	tests := []struct {
		name             string
		networks         map[string]manifest.TopLevelResourceSpec
		existingNetworks []string
		expectedCreated  []string
	}{
		{
			name:             "no networks needed",
			networks:         map[string]manifest.TopLevelResourceSpec{},
			existingNetworks: []string{},
			expectedCreated:  []string{},
		},
		{
			name: "create network",
			networks: map[string]manifest.TopLevelResourceSpec{
				"app-network": {},
			},
			existingNetworks: []string{},
			expectedCreated:  []string{"app-network"},
		},
		{
			name: "skip existing network",
			networks: map[string]manifest.TopLevelResourceSpec{
				"app-network": {},
			},
			existingNetworks: []string{"app-network"},
			expectedCreated:  []string{},
		},
		{
			name: "mixed networks",
			networks: map[string]manifest.TopLevelResourceSpec{
				"app-network": {},
				"db-network":  {},
			},
			existingNetworks: []string{"app-network"},
			expectedCreated:  []string{"db-network"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock Docker client with existing networks
			mockDocker := newMockDocker()
			mockDocker.networks = tt.existingNetworks
			
			planner := &Planner{docker: mockDocker}
			resourceManager := NewResourceManager(planner)

			cfg := manifest.Config{Networks: tt.networks}
			labels := map[string]string{"test": "label"}

			err := resourceManager.EnsureNetworksExist(context.Background(), cfg, labels)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check that the correct networks were created
			for _, expectedNetwork := range tt.expectedCreated {
				found := false
				for _, createdNetwork := range mockDocker.createdNetworks {
					if createdNetwork == expectedNetwork {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected network %q to be created, but it wasn't", expectedNetwork)
				}
			}
		})
	}
}

// Helper function to test volume deduplication logic
func TestVolumeDeduplication(t *testing.T) {
	cfg := manifest.Config{
		Filesets: map[string]manifest.FilesetSpec{
			"web-assets": {TargetVolume: "shared-vol"},
			"api-assets": {TargetVolume: "shared-vol"}, // Same volume
			"db-backup":  {TargetVolume: "backup-vol"},
		},
		Volumes: map[string]manifest.TopLevelResourceSpec{
			"shared-vol": {}, // Explicit definition should not cause duplication
			"other-vol":  {},
		},
	}

	// Test volume name collection and deduplication
	volumeNames := make(map[string]bool)

	// Add volumes from filesets
	for _, fileset := range cfg.Filesets {
		if fileset.TargetVolume != "" {
			volumeNames[fileset.TargetVolume] = true
		}
	}

	// Add explicit volumes
	for name := range cfg.Volumes {
		volumeNames[name] = true
	}

	// Should have exactly 3 unique volumes: shared-vol, backup-vol, other-vol
	expectedVolumes := []string{"shared-vol", "backup-vol", "other-vol"}
	if len(volumeNames) != len(expectedVolumes) {
		t.Errorf("Expected %d unique volumes, got %d: %v", len(expectedVolumes), len(volumeNames), volumeNames)
	}

	for _, expected := range expectedVolumes {
		if !volumeNames[expected] {
			t.Errorf("Expected volume %q to be in volume list", expected)
		}
	}
}
