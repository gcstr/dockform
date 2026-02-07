package planner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
)

// BenchmarkBuildPlanSequential measures the performance of sequential BuildPlan operation
func BenchmarkBuildPlanSequential(b *testing.B) {
	benchmarkBuildPlan(b, false)
}

// BenchmarkBuildPlanParallel measures the performance of parallel BuildPlan operation
func BenchmarkBuildPlanParallel(b *testing.B) {
	benchmarkBuildPlan(b, true)
}

// benchmarkBuildPlan is the common benchmark function for both modes
func benchmarkBuildPlan(b *testing.B, parallel bool) {
	// Create a mock Docker client for testing with realistic data
	docker := newMockDocker()
	docker.volumes = []string{"vol1", "vol2", "vol3", "existing-volume"}
	docker.networks = []string{"net1", "net2", "net3", "existing-network"}

	planner := NewWithDocker(docker).WithParallel(parallel)
	sourceBase := b.TempDir()
	assets1Dir := filepath.Join(sourceBase, "assets1")
	assets2Dir := filepath.Join(sourceBase, "assets2")
	configDir := filepath.Join(sourceBase, "config")
	for _, dir := range []string{assets1Dir, assets2Dir, configDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("sample"), 0o644); err != nil {
			b.Fatalf("write sample file in %s: %v", dir, err)
		}
	}

	// Create a test configuration with multiple applications and filesets
	cfg := manifest.Config{
		Identifier: "benchmark-test",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
		Stacks: map[string]manifest.Stack{
			"default/app1": {
				Root:  "/tmp/app1",
				Files: []string{"docker-compose.yml"},
				Environment: &manifest.Environment{
					Inline: []string{"PORT=3000"},
				},
			},
			"default/app2": {
				Root:  "/tmp/app2",
				Files: []string{"docker-compose.yml"},
				Environment: &manifest.Environment{
					Inline: []string{"PORT=3001"},
				},
			},
			"default/app3": {
				Root:  "/tmp/app3",
				Files: []string{"docker-compose.yml"},
				Environment: &manifest.Environment{
					Inline: []string{"PORT=3002"},
				},
			},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{
			"assets1": {
				Source:       "./assets1",
				SourceAbs:    assets1Dir,
				TargetVolume: "assets-vol1",
				TargetPath:   "/var/www/assets",
				Exclude:      []string{".git"},
			},
			"assets2": {
				Source:       "./assets2",
				SourceAbs:    assets2Dir,
				TargetVolume: "assets-vol2",
				TargetPath:   "/var/www/assets2",
				Exclude:      []string{".git"},
			},
			"config": {
				Source:       "./config",
				SourceAbs:    configDir,
				TargetVolume: "config-vol",
				TargetPath:   "/etc/app",
				Exclude:      []string{"*.tmp"},
			},
		},
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := planner.BuildPlan(ctx, cfg)
		if err != nil {
			b.Fatalf("BuildPlan failed: %v", err)
		}
	}
}

// BenchmarkBuildPlanLargeSequential tests with a larger configuration using sequential processing
func BenchmarkBuildPlanLargeSequential(b *testing.B) {
	benchmarkBuildPlanLarge(b, false)
}

// BenchmarkBuildPlanLargeParallel tests with a larger configuration using parallel processing
func BenchmarkBuildPlanLargeParallel(b *testing.B) {
	benchmarkBuildPlanLarge(b, true)
}

// benchmarkBuildPlanLarge is the common benchmark function for large configurations
func benchmarkBuildPlanLarge(b *testing.B, parallel bool) {
	docker := newMockDocker()

	// Add many existing volumes and networks to simulate real environments
	for i := 0; i < 20; i++ {
		docker.volumes = append(docker.volumes, fmt.Sprintf("existing-vol-%d", i))
		docker.networks = append(docker.networks, fmt.Sprintf("existing-net-%d", i))
	}

	planner := NewWithDocker(docker).WithParallel(parallel)
	sourceBase := b.TempDir()

	// Create a large configuration with many applications and filesets
	applications := make(map[string]manifest.Stack)
	filesets := make(map[string]manifest.FilesetSpec)

	// Add 10 applications
	for i := 0; i < 10; i++ {
		applications[fmt.Sprintf("default/app%d", i)] = manifest.Stack{
			Root:  fmt.Sprintf("/tmp/app%d", i),
			Files: []string{"docker-compose.yml"},
			Environment: &manifest.Environment{
				Inline: []string{fmt.Sprintf("PORT=%d", 3000+i)},
			},
		}
	}

	// Add 15 filesets
	for i := 0; i < 15; i++ {
		sourceAbs := filepath.Join(sourceBase, fmt.Sprintf("assets%d", i))
		if err := os.MkdirAll(sourceAbs, 0o755); err != nil {
			b.Fatalf("mkdir %s: %v", sourceAbs, err)
		}
		if err := os.WriteFile(filepath.Join(sourceAbs, "sample.txt"), []byte("sample"), 0o644); err != nil {
			b.Fatalf("write sample file in %s: %v", sourceAbs, err)
		}
		filesets[fmt.Sprintf("assets%d", i)] = manifest.FilesetSpec{
			Source:       fmt.Sprintf("./assets%d", i),
			SourceAbs:    sourceAbs,
			TargetVolume: fmt.Sprintf("assets-vol%d", i),
			TargetPath:   fmt.Sprintf("/var/www/assets%d", i),
			Exclude:      []string{".git", "*.tmp"},
		}
	}

	cfg := manifest.Config{
		Identifier: "benchmark-large",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
		Stacks:             applications,
		DiscoveredFilesets: filesets,
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := planner.BuildPlan(ctx, cfg)
		if err != nil {
			b.Fatalf("BuildPlan failed: %v", err)
		}
	}
}
