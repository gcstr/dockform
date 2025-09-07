package planner

import (
	"context"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
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
		Applications: map[string]manifest.Application{
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
		Networks: map[string]manifest.TopLevelResourceSpec{
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
	sequentialLines := sequentialPlan.Lines
	parallelLines := parallelPlan.Lines
	
	if len(sequentialLines) != len(parallelLines) {
		t.Errorf("Different number of plan lines: sequential=%d, parallel=%d", 
			len(sequentialLines), len(parallelLines))
		return
	}
	
	// Count different types of operations to ensure they match
	sequentialCounts := countLineTypes(sequentialLines)
	parallelCounts := countLineTypes(parallelLines)
	
	for lineType, seqCount := range sequentialCounts {
		parCount, exists := parallelCounts[lineType]
		if !exists || seqCount != parCount {
			t.Errorf("Mismatch in %s operations: sequential=%d, parallel=%d", 
				lineType, seqCount, parCount)
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
	
	app := manifest.Application{
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
	
	// Default should be false
	if planner.parallel {
		t.Error("Default parallel setting should be false")
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
	
	// Default should be false
	if detector.parallel {
		t.Error("Default parallel setting should be false")
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

// countLineTypes counts different types of UI operations for comparison
func countLineTypes(lines []ui.DiffLine) map[string]int {
	counts := make(map[string]int)
	for _, line := range lines {
		// Create a key based on operation type and general content
		text := line.Message
		var key string
		switch line.Type {
		case ui.Add:
			if strings.Contains(text, "volume") {
				key = "add_volume"
			} else if strings.Contains(text, "network") {
				key = "add_network"
			} else if strings.Contains(text, "service") {
				key = "add_service"
			} else {
				key = "add_other"
			}
		case ui.Remove:
			if strings.Contains(text, "volume") {
				key = "remove_volume"
			} else if strings.Contains(text, "network") {
				key = "remove_network"
			} else if strings.Contains(text, "service") {
				key = "remove_service"
			} else {
				key = "remove_other"
			}
		case ui.Change:
			if strings.Contains(text, "fileset") {
				key = "change_fileset"
			} else if strings.Contains(text, "service") {
				key = "change_service"
			} else {
				key = "change_other"
			}
		case ui.Noop:
			if strings.Contains(text, "application") {
				key = "noop_application"
			} else if strings.Contains(text, "fileset") {
				key = "noop_fileset"
			} else if strings.Contains(text, "service") {
				key = "noop_service"
			} else {
				key = "noop_other"
			}
		default:
			key = "unknown"
		}
		counts[key]++
	}
	return counts
}
