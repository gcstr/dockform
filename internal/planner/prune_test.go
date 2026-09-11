package planner

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
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

// TestPlanner_Prune_PreservesComposeOwnedNetworks verifies that networks created
// by a compose stack (carrying the identifier label but managed by the stack) are
// not pruned as orphans, while genuinely unmanaged networks still are. Regression
// test for GH #54.
func TestPlanner_Prune_PreservesComposeOwnedNetworks(t *testing.T) {
	mock := newMockDocker()
	mock.networks = []string{"whoami", "stale-net"}
	mock.composeNetworks = map[string]string{"whoami": "whoami"} // owned by the active whoami stack

	p := NewWithDocker(mock)

	cfg := manifest.Config{
		Identifier: "test",
		Contexts:   map[string]manifest.ContextConfig{"default": {}},
		Stacks:     map[string]manifest.Stack{"default/whoami": {Root: filepath.Join(t.TempDir(), "whoami"), Files: []string{"compose.yml"}}},
	}

	if err := p.Prune(context.Background(), cfg); err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	if len(mock.removedNetworks) != 1 || mock.removedNetworks[0] != "stale-net" {
		t.Errorf("expected only stale-net removed (compose-owned whoami preserved), got %v", mock.removedNetworks)
	}
}

// A compose network whose stack has been removed from the manifest is no longer
// protected: its containers are pruned, so the network must go too (dockform-x59).
func TestPlanner_Prune_RemovesComposeNetworkOfRemovedStack(t *testing.T) {
	mock := newMockDocker()
	mock.networks = []string{"app_default", "gone_default", "stale-net"}
	mock.composeNetworks = map[string]string{"app_default": "app", "gone_default": "gone"}

	p := NewWithDocker(mock)
	cfg := manifest.Config{
		Identifier: "test",
		Contexts:   map[string]manifest.ContextConfig{"default": {}},
		Stacks:     map[string]manifest.Stack{"default/app": {Root: filepath.Join(t.TempDir(), "app"), Files: []string{"compose.yml"}}},
	}
	if err := p.Prune(context.Background(), cfg); err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
	got := append([]string(nil), mock.removedNetworks...)
	sort.Strings(got)
	if want := []string{"gone_default", "stale-net"}; !reflect.DeepEqual(got, want) {
		t.Errorf("removed networks = %v, want %v (app_default belongs to the active stack)", got, want)
	}
}

// A stack whose compose file sets name: (or COMPOSE_PROJECT_NAME) runs under
// that project, not its directory name, so its network must be kept.
func TestPlanner_Prune_KeepsNetworkOfStackWithComposeName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "app")
	mock := newMockDocker()
	mock.networks = []string{"custom_default", "app_default"}
	mock.composeNetworks = map[string]string{"custom_default": "custom", "app_default": "app"}
	mock.composeProjectNames = map[string]string{root: "custom"}

	p := NewWithDocker(mock)
	cfg := manifest.Config{
		Identifier: "test",
		Contexts:   map[string]manifest.ContextConfig{"default": {}},
		Stacks:     map[string]manifest.Stack{"default/app": {Root: root, Files: []string{"compose.yml"}}},
	}
	if err := p.Prune(context.Background(), cfg); err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
	if want := []string{"app_default"}; !reflect.DeepEqual(mock.removedNetworks, want) {
		t.Errorf("removed networks = %v, want %v (custom_default belongs to the active stack)", mock.removedNetworks, want)
	}
}

// When any stack's project cannot be resolved, prune cannot tell an active
// compose network from a stale one, so it keeps all of them.
func TestPlanner_Prune_UnresolvedProjectKeepsComposeNetworks(t *testing.T) {
	mock := newMockDocker()
	mock.networks = []string{"gone_default", "stale-net"}
	mock.composeNetworks = map[string]string{"gone_default": "gone"}
	mock.composeConfigFullError = errors.New("compose config failed")

	p := NewWithDocker(mock)
	cfg := manifest.Config{
		Identifier: "test",
		Contexts:   map[string]manifest.ContextConfig{"default": {}},
		Stacks:     map[string]manifest.Stack{"default/app": {Root: filepath.Join(t.TempDir(), "app"), Files: []string{"compose.yml"}}},
	}
	_ = p.Prune(context.Background(), cfg)
	if want := []string{"stale-net"}; !reflect.DeepEqual(mock.removedNetworks, want) {
		t.Errorf("removed networks = %v, want %v", mock.removedNetworks, want)
	}
}
