package planner

import (
	"context"
	"fmt"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

// TestParallelVsSequentialSameResults verifies that parallel processing produces the same results as sequential
func TestParallelVsSequentialSameResults(t *testing.T) {
	// Create a mock Docker client with test data
	docker := newMockDocker()
	docker.volumes = []string{"existing-vol1", "existing-vol2"}
	docker.networks = []string{"existing-net1", "existing-net2"}

	// Add some mock volume file content
	docker.volumeFiles = map[string]string{
		"assets-vol": `{"tree_hash":"test123","files":{"file1.txt":"hash1"}}`,
	}

	// Create test configuration with multiple applications
	cfg := manifest.Config{
		Docker: manifest.DockerConfig{
			Context:    "default",
			Identifier: "parallel-test",
		},
		Stacks: map[string]manifest.Stack{
			"app1": {
				Root:  "/tmp/app1",
				Files: []string{"docker-compose.yml"},
				Environment: &manifest.Environment{
					Inline: []string{"ENV=test"},
				},
			},
			"app2": {
				Root:  "/tmp/app2",
				Files: []string{"docker-compose.yml"},
				Environment: &manifest.Environment{
					Inline: []string{"ENV=prod"},
				},
			},
			"app3": {
				Root:  "/tmp/app3",
				Files: []string{"docker-compose.yml"},
				Environment: &manifest.Environment{
					Inline: []string{"ENV=dev"},
				},
			},
		},
		Volumes: map[string]manifest.TopLevelResourceSpec{
			"shared-vol": {},
		},
		Networks: map[string]manifest.NetworkSpec{
			"app-network": {},
		},
		Filesets: map[string]manifest.FilesetSpec{
			"assets": {
				Source:       "./assets",
				SourceAbs:    "/tmp/assets",
				TargetVolume: "assets-vol",
				TargetPath:   "/var/www/assets",
				Exclude:      []string{".git"},
			},
		},
	}

	ctx := context.Background()

	// Test sequential processing
	sequentialPlanner := NewWithDocker(docker).WithParallel(false)
	sequentialPlan, err := sequentialPlanner.BuildPlan(ctx, cfg)
	if err != nil {
		t.Fatalf("Sequential BuildPlan failed: %v", err)
	}

	// Test parallel processing
	parallelPlanner := NewWithDocker(docker).WithParallel(true)
	parallelPlan, err := parallelPlanner.BuildPlan(ctx, cfg)
	if err != nil {
		t.Fatalf("Parallel BuildPlan failed: %v", err)
	}

	// Compare the results - for this test we'll compare the structure rather than exact output
	// since removal ordering can be non-deterministic in parallel processing
	sequentialResources := sequentialPlan.Resources.AllResources()
	parallelResources := parallelPlan.Resources.AllResources()

	if len(sequentialResources) != len(parallelResources) {
		t.Errorf("Different number of plan resources: sequential=%d, parallel=%d",
			len(sequentialResources), len(parallelResources))
		return
	}

	// Count different types of operations to ensure they match
	sequentialCounts := countResourceActions(sequentialResources)
	parallelCounts := countResourceActions(parallelResources)

	for action, seqCount := range sequentialCounts {
		parCount, exists := parallelCounts[action]
		if !exists || seqCount != parCount {
			t.Errorf("Mismatch in %s operations: sequential=%d, parallel=%d",
				action, seqCount, parCount)
		}
	}
}

// TestServiceStateDetectorParallel tests the parallel service state detection specifically
func TestServiceStateDetectorParallel(t *testing.T) {
	docker := newMockDocker()

	// Mock some running containers
	docker.composePsItems = []dockercli.ComposePsItem{
		{Name: "app1-service1", Service: "service1"},
		{Name: "app1-service2", Service: "service2"},
	}

	// Mock container labels
	docker.containerLabels = map[string]map[string]string{
		"app1-service1": {
			"com.docker.compose.config-hash": "hash123",
			"io.dockform.identifier":         "test-id",
		},
		"app1-service2": {
			"com.docker.compose.config-hash": "hash456",
			"io.dockform.identifier":         "test-id",
		},
	}

	app := manifest.Stack{
		Root:  "/tmp/app1",
		Files: []string{"docker-compose.yml"},
		Environment: &manifest.Environment{
			Inline: []string{"ENV=test"},
		},
	}

	ctx := context.Background()

	// Test sequential processing
	sequentialDetector := NewServiceStateDetector(docker).WithParallel(false)
	sequentialResults, err := sequentialDetector.DetectAllServicesState(ctx, "app1", app, "test-id", nil)
	if err != nil {
		t.Fatalf("Sequential DetectAllServicesState failed: %v", err)
	}

	// Test parallel processing
	parallelDetector := NewServiceStateDetector(docker).WithParallel(true)
	parallelResults, err := parallelDetector.DetectAllServicesState(ctx, "app1", app, "test-id", nil)
	if err != nil {
		t.Fatalf("Parallel DetectAllServicesState failed: %v", err)
	}

	// Compare results
	if len(sequentialResults) != len(parallelResults) {
		t.Errorf("Different number of results: sequential=%d, parallel=%d",
			len(sequentialResults), len(parallelResults))
		return
	}

	// Compare each service result (order should be maintained)
	for i := range sequentialResults {
		seq := sequentialResults[i]
		par := parallelResults[i]

		if seq.Name != par.Name {
			t.Errorf("Service %d name mismatch: sequential=%s, parallel=%s", i, seq.Name, par.Name)
		}
		if seq.State != par.State {
			t.Errorf("Service %s state mismatch: sequential=%d, parallel=%d", seq.Name, seq.State, par.State)
		}
		if seq.DesiredHash != par.DesiredHash {
			t.Errorf("Service %s desired hash mismatch: sequential=%s, parallel=%s", seq.Name, seq.DesiredHash, par.DesiredHash)
		}
		if seq.RunningHash != par.RunningHash {
			t.Errorf("Service %s running hash mismatch: sequential=%s, parallel=%s", seq.Name, seq.RunningHash, par.RunningHash)
		}
	}
}

// TestPlannerParallelConfiguration tests the parallel configuration methods
func TestPlannerParallelConfiguration(t *testing.T) {
	planner := New()

	// Default should be true
	if !planner.parallel {
		t.Error("Default parallel setting should be true")
	}

	// Test enabling parallel
	planner = planner.WithParallel(true)
	if !planner.parallel {
		t.Error("WithParallel(true) should enable parallel processing")
	}

	// Test disabling parallel
	planner = planner.WithParallel(false)
	if planner.parallel {
		t.Error("WithParallel(false) should disable parallel processing")
	}
}

// TestServiceStateDetectorConfiguration tests the parallel configuration for service detector
func TestServiceStateDetectorConfiguration(t *testing.T) {
	docker := newMockDocker()
	detector := NewServiceStateDetector(docker)

	// Default should be true
	if !detector.parallel {
		t.Error("Default parallel setting should be true")
	}

	// Test enabling parallel
	detector = detector.WithParallel(true)
	if !detector.parallel {
		t.Error("WithParallel(true) should enable parallel processing")
	}

	// Test disabling parallel
	detector = detector.WithParallel(false)
	if detector.parallel {
		t.Error("WithParallel(false) should disable parallel processing")
	}
}

// countResourceActions counts different types of resource actions for comparison
func countResourceActions(resources []Resource) map[string]int {
	counts := make(map[string]int)
	for _, comp := range resources {
		// Create a key based on resource type and action
		key := fmt.Sprintf("%s_%s", comp.Type, comp.Action)
		counts[key]++
	}
	return counts
}
