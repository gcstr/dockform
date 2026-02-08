package planner

import (
	"context"
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
)

func TestResourceManager_EnsureVolumesExistForContext(t *testing.T) {
	tests := []struct {
		name                string
		filesets            map[string]manifest.FilesetSpec
		existingVolumes     []string
		expectedCreated     []string
		expectedVolumeCount int
	}{
		{
			name:                "no volumes needed",
			filesets:            map[string]manifest.FilesetSpec{},
			existingVolumes:     []string{},
			expectedCreated:     []string{},
			expectedVolumeCount: 0,
		},
		{
			name: "create fileset volume",
			filesets: map[string]manifest.FilesetSpec{
				"default/web/data": {TargetVolume: "app-data", Context: "default"},
			},
			existingVolumes:     []string{},
			expectedCreated:     []string{"app-data"},
			expectedVolumeCount: 1,
		},
		{
			name: "skip existing volume",
			filesets: map[string]manifest.FilesetSpec{
				"default/web/data": {TargetVolume: "app-data", Context: "default"},
			},
			existingVolumes:     []string{"app-data"},
			expectedCreated:     []string{},
			expectedVolumeCount: 1, // existing volume is returned in the map
		},
		{
			name: "multiple filesets same volume",
			filesets: map[string]manifest.FilesetSpec{
				"default/web/assets": {TargetVolume: "shared-data", Context: "default"},
				"default/api/assets": {TargetVolume: "shared-data", Context: "default"}, // Same volume
			},
			existingVolumes:     []string{},
			expectedCreated:     []string{"shared-data"},
			expectedVolumeCount: 1, // Deduplicated
		},
		{
			name: "multiple filesets different volumes",
			filesets: map[string]manifest.FilesetSpec{
				"default/web/assets": {TargetVolume: "web-data", Context: "default"},
				"default/api/data":   {TargetVolume: "api-data", Context: "default"},
			},
			existingVolumes:     []string{},
			expectedCreated:     []string{"web-data", "api-data"},
			expectedVolumeCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock Docker client with existing volumes
			mockDocker := newMockDocker()
			mockDocker.volumes = tt.existingVolumes

			resourceManager := NewResourceManagerWithClient(mockDocker, nil)

			cfg := manifest.Config{
				Identifier: "test-id",
				Contexts: map[string]manifest.ContextConfig{
					"default": {},
				},
				DiscoveredFilesets: tt.filesets,
			}
			labels := map[string]string{"test": "label"}

			resultVolumes, err := resourceManager.EnsureVolumesExistForContext(context.Background(), cfg, "default", labels)
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

// Helper function to test volume deduplication logic from filesets
func TestVolumeDeduplicationFromFilesets(t *testing.T) {
	cfg := manifest.Config{
		Identifier: "test-id",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{
			"default/web/assets": {TargetVolume: "shared-vol", Context: "default"},
			"default/api/assets": {TargetVolume: "shared-vol", Context: "default"}, // Same volume
			"default/db/backup":  {TargetVolume: "backup-vol", Context: "default"},
		},
	}

	// Test volume name collection and deduplication
	volumeNames := make(map[string]bool)

	// Add volumes from filesets
	for _, fileset := range cfg.GetAllFilesets() {
		if fileset.TargetVolume != "" {
			volumeNames[fileset.TargetVolume] = true
		}
	}

	// Should have exactly 2 unique volumes: shared-vol, backup-vol
	expectedVolumes := []string{"shared-vol", "backup-vol"}
	if len(volumeNames) != len(expectedVolumes) {
		t.Errorf("Expected %d unique volumes, got %d: %v", len(expectedVolumes), len(volumeNames), volumeNames)
	}

	for _, expected := range expectedVolumes {
		if !volumeNames[expected] {
			t.Errorf("Expected volume %q to be in volume list", expected)
		}
	}
}

func TestGetFilesetsForContext(t *testing.T) {
	cfg := manifest.Config{
		Identifier: "test-id",
		Contexts: map[string]manifest.ContextConfig{
			"local":  {},
			"remote": {},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{
			"local/web/assets":   {TargetVolume: "web-data", Context: "local"},
			"local/api/data":     {TargetVolume: "api-data", Context: "local"},
			"remote/prod/config": {TargetVolume: "prod-config", Context: "remote"},
		},
	}

	localFilesets := cfg.GetFilesetsForContext("local")
	if len(localFilesets) != 2 {
		t.Errorf("Expected 2 filesets for local context, got %d", len(localFilesets))
	}

	remoteFilesets := cfg.GetFilesetsForContext("remote")
	if len(remoteFilesets) != 1 {
		t.Errorf("Expected 1 fileset for remote context, got %d", len(remoteFilesets))
	}

	nonexistentFilesets := cfg.GetFilesetsForContext("nonexistent")
	if len(nonexistentFilesets) != 0 {
		t.Errorf("Expected 0 filesets for nonexistent context, got %d", len(nonexistentFilesets))
	}
}
