package planner

import (
	"context"
	"fmt"
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

	// Create a test configuration with multiple applications and filesets
	cfg := manifest.Config{
		Docker: manifest.DockerConfig{
			Context:    "default",
			Identifier: "benchmark-test",
		},
		Applications: map[string]manifest.Application{
			"app1": {
				Root:  "/tmp/app1",
				Files: []string{"docker-compose.yml"},
				Environment: &manifest.Environment{
					Inline: []string{"PORT=3000"},
				},
			},
			"app2": {
				Root:  "/tmp/app2",
				Files: []string{"docker-compose.yml"},
				Environment: &manifest.Environment{
					Inline: []string{"PORT=3001"},
				},
			},
			"app3": {
				Root:  "/tmp/app3",
				Files: []string{"docker-compose.yml"},
				Environment: &manifest.Environment{
					Inline: []string{"PORT=3002"},
				},
			},
		},
		Volumes: map[string]manifest.TopLevelResourceSpec{
			"shared-vol1": {},
			"shared-vol2": {},
		},
		Networks: map[string]manifest.NetworkSpec{
			"app-network": {},
		},
		Filesets: map[string]manifest.FilesetSpec{
			"assets1": {
				Source:       "./assets1",
				SourceAbs:    "/tmp/assets1",
				TargetVolume: "assets-vol1",
				TargetPath:   "/var/www/assets",
				Exclude:      []string{".git"},
			},
			"assets2": {
				Source:       "./assets2",
				SourceAbs:    "/tmp/assets2",
				TargetVolume: "assets-vol2",
				TargetPath:   "/var/www/assets2",
				Exclude:      []string{".git"},
			},
			"config": {
				Source:       "./config",
				SourceAbs:    "/tmp/config",
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

	// Create a large configuration with many applications and filesets
	applications := make(map[string]manifest.Application)
	filesets := make(map[string]manifest.FilesetSpec)
	volumes := make(map[string]manifest.TopLevelResourceSpec)
	networks := make(map[string]manifest.NetworkSpec)

	// Add 10 applications
	for i := 0; i < 10; i++ {
		applications[fmt.Sprintf("app%d", i)] = manifest.Application{
			Root:  fmt.Sprintf("/tmp/app%d", i),
			Files: []string{"docker-compose.yml"},
			Environment: &manifest.Environment{
				Inline: []string{fmt.Sprintf("PORT=%d", 3000+i)},
			},
		}
	}

	// Add 15 filesets
	for i := 0; i < 15; i++ {
		filesets[fmt.Sprintf("assets%d", i)] = manifest.FilesetSpec{
			Source:       fmt.Sprintf("./assets%d", i),
			SourceAbs:    fmt.Sprintf("/tmp/assets%d", i),
			TargetVolume: fmt.Sprintf("assets-vol%d", i),
			TargetPath:   fmt.Sprintf("/var/www/assets%d", i),
			Exclude:      []string{".git", "*.tmp"},
		}
	}

	// Add 5 volumes
	for i := 0; i < 5; i++ {
		volumes[fmt.Sprintf("shared-vol%d", i)] = manifest.TopLevelResourceSpec{}
	}

	// Add 5 networks
	for i := 0; i < 5; i++ {
		networks[fmt.Sprintf("app-network%d", i)] = manifest.NetworkSpec{}
	}

	cfg := manifest.Config{
		Docker: manifest.DockerConfig{
			Context:    "default",
			Identifier: "benchmark-large",
		},
		Applications: applications,
		Volumes:      volumes,
		Networks:     networks,
		Filesets:     filesets,
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
