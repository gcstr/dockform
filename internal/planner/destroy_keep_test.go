package planner

import (
	"context"
	"slices"
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
)

// keepCfg declares volume "data" and network "keepnet" with destroy: false.
func keepCfg() manifest.Config {
	no := false
	return manifest.Config{
		Identifier: "test",
		Contexts: map[string]manifest.ContextConfig{
			"default": {
				Volumes:  map[string]manifest.TopLevelResourceSpec{"data": {Destroy: &no}, "scratch": {}},
				Networks: map[string]manifest.NetworkSpec{"keepnet": {Destroy: &no}},
			},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{},
	}
}

func keepMock() *mockDockerClient {
	m := newMockDocker()
	m.volumes = []string{"data", "scratch"}
	m.networks = []string{"keepnet", "plainnet"}
	return m
}

// actionsByName flattens a destroy plan's volumes and networks.
func actionsByName(p *Plan) map[string]Action {
	got := map[string]Action{}
	for _, r := range p.Resources.Volumes {
		got["volume "+r.Name] = r.Action
	}
	for _, r := range p.Resources.Networks {
		got["network "+r.Name] = r.Action
	}
	return got
}

func TestBuildDestroyPlan_MarksFlaggedResourcesKept(t *testing.T) {
	p, err := NewWithDocker(keepMock()).BuildDestroyPlan(context.Background(), keepCfg())
	if err != nil {
		t.Fatalf("build destroy plan: %v", err)
	}
	got := actionsByName(p)
	want := map[string]Action{
		"volume data":      ActionKeep,
		"volume scratch":   ActionDelete,
		"network keepnet":  ActionKeep,
		"network plainnet": ActionDelete,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: action %q, want %q (plan: %v)", k, got[k], w, got)
		}
	}
}

// The plan and the executor must agree: what the plan calls kept, destroy
// must never remove — and it must still remove everything else.
func TestDestroy_NeverRemovesFlaggedResources(t *testing.T) {
	m := keepMock()
	if err := NewWithDocker(m).Destroy(context.Background(), keepCfg()); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if slices.Contains(m.removedVolumes, "data") {
		t.Errorf("kept volume data was removed: %v", m.removedVolumes)
	}
	if !slices.Contains(m.removedVolumes, "scratch") {
		t.Errorf("unflagged volume scratch was not removed: %v", m.removedVolumes)
	}
	if slices.Contains(m.removedNetworks, "keepnet") {
		t.Errorf("kept network keepnet was removed: %v", m.removedNetworks)
	}
	if !slices.Contains(m.removedNetworks, "plainnet") {
		t.Errorf("unflagged network plainnet was not removed: %v", m.removedNetworks)
	}
}

// The flag wins over fileset targeting: a kept volume that a fileset writes
// into is still kept, in the plan and in the executor.
func TestDestroy_KeepWinsOverFilesetTargeting(t *testing.T) {
	cfg := keepCfg()
	cfg.DiscoveredFilesets = map[string]manifest.FilesetSpec{
		"default/app/html": {TargetVolume: "data", TargetPath: "/usr/share/nginx/html"},
	}

	p, err := NewWithDocker(keepMock()).BuildDestroyPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build destroy plan: %v", err)
	}
	if got := actionsByName(p)["volume data"]; got != ActionKeep {
		t.Errorf("fileset-target volume data: action %q, want %q", got, ActionKeep)
	}
	for name, items := range p.Resources.Filesets {
		for _, it := range items {
			if it.Action == ActionDelete {
				t.Errorf("fileset %s still schedules a delete for a kept volume: %+v", name, it)
			}
		}
	}

	m := keepMock()
	if err := NewWithDocker(m).Destroy(context.Background(), cfg); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if slices.Contains(m.removedVolumes, "data") {
		t.Errorf("kept fileset-target volume data was removed: %v", m.removedVolumes)
	}
}

// Under a scoped destroy, context-level volumes are never touched, so a kept
// one produces no plan line at all and is never removed.
func TestDestroy_ScopedDestroyNeitherListsNorRemovesKeptContextVolume(t *testing.T) {
	cfg := keepCfg()
	cfg.Targeted = true
	cfg.Stacks = map[string]manifest.Stack{}

	p, err := NewWithDocker(keepMock()).BuildDestroyPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build destroy plan: %v", err)
	}
	if a, ok := actionsByName(p)["volume data"]; ok {
		t.Errorf("scoped destroy listed kept context volume data with action %q; want no line", a)
	}

	m := keepMock()
	if err := NewWithDocker(m).Destroy(context.Background(), cfg); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if slices.Contains(m.removedVolumes, "data") {
		t.Errorf("scoped destroy removed kept context volume data: %v", m.removedVolumes)
	}
}
