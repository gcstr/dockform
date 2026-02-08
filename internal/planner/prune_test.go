package planner

import (
	"context"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

func TestPlanner_Prune_NoClientConfigured(t *testing.T) {
	p := &Planner{} // No docker client or factory
	cfg := manifest.Config{
		Identifier: "test",
		Contexts:   map[string]manifest.ContextConfig{"default": {}},
	}

	err := p.Prune(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for unconfigured docker client")
	}
	if !apperr.IsKind(err, apperr.Precondition) {
		t.Errorf("expected Precondition error, got %v", err)
	}
}

func TestPlanner_PruneWithPlan_NoClientConfigured(t *testing.T) {
	p := &Planner{}
	cfg := manifest.Config{
		Identifier: "test",
		Contexts:   map[string]manifest.ContextConfig{"default": {}},
	}

	err := p.PruneWithPlan(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected error for unconfigured docker client")
	}
}

func TestPlanner_Prune_RemovesOrphanedVolumes(t *testing.T) {
	mock := newMockDocker()
	mock.volumes = []string{"orphan-vol", "kept-vol"}
	mock.containers = []dockercli.PsBrief{}

	p := NewWithDocker(mock)

	cfg := manifest.Config{
		Identifier: "test",
		Contexts:   map[string]manifest.ContextConfig{"default": {}},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{
			"fs1": {TargetVolume: "kept-vol", Context: "default"},
		},
	}

	err := p.Prune(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	// Check orphaned volume was removed
	if len(mock.removedVolumes) != 1 || mock.removedVolumes[0] != "orphan-vol" {
		t.Errorf("expected orphan-vol to be removed, got %v", mock.removedVolumes)
	}
}

func TestPlanner_Prune_RemovesOrphanedContainers(t *testing.T) {
	mock := newMockDocker()
	mock.containers = []dockercli.PsBrief{
		{Name: "orphan-container", Project: "old", Service: "orphan-svc"},
	}
	mock.volumes = []string{}

	p := NewWithDocker(mock)

	cfg := manifest.Config{
		Identifier: "test",
		Contexts:   map[string]manifest.ContextConfig{"default": {}},
	}

	err := p.Prune(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	// Container should be removed since there are no stacks
	if len(mock.removedContainers) != 1 {
		t.Errorf("expected 1 container to be removed, got %d", len(mock.removedContainers))
	}
}
