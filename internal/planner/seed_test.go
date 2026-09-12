package planner

import (
	"context"
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
)

func fixtureResourcePlan() *ResourcePlan {
	return &ResourcePlan{
		Volumes: []Resource{
			NewResource(ResourceVolume, "data", ActionCreate, ""),
			NewResource(ResourceVolume, "cache", ActionNoop, "exists"),
		},
		Networks: []Resource{
			NewResource(ResourceNetwork, "proxy", ActionCreate, ""),
		},
		// Deliberately inserted out of alphabetical order: seeding must sort.
		Stacks: map[string][]Resource{
			"traefik": {
				NewNestedResource(ResourceService, "traefik", "traefik", ActionCreate, ""),
			},
			"authelia": {
				NewNestedResource(ResourceService, "authelia", "authelia", ActionCreate, ""),
				NewNestedResource(ResourceService, "redis", "authelia", ActionNoop, "up to date"),
			},
			"quiet": {
				NewNestedResource(ResourceService, "nothing", "quiet", ActionNoop, "up to date"),
			},
		},
		Filesets: map[string][]Resource{
			"web_config": {
				NewNestedResource(ResourceFile, "index.html", "web_config", ActionUpdate, ""),
				NewNestedResource(ResourceFile, "style.css", "web_config", ActionCreate, ""),
			},
			"unchanged": {
				NewNestedResource(ResourceFile, "", "unchanged", ActionNoop, "no file changes"),
			},
		},
		Containers: []Resource{
			NewResource(ResourceContainer, "stale", ActionDelete, ""),
		},
	}
}

func TestSeedRefsOrderAndFiltering(t *testing.T) {
	got := SeedRefs("hetzner-two", fixtureResourcePlan())

	want := []ResourceRef{
		{Context: "hetzner-two", Type: ResourceVolume, Name: "data"},
		{Context: "hetzner-two", Type: ResourceNetwork, Name: "proxy"},
		{Context: "hetzner-two", Type: ResourceStack, Name: "authelia"},
		{Context: "hetzner-two", Type: ResourceService, Name: "authelia", Parent: "authelia"},
		{Context: "hetzner-two", Type: ResourceStack, Name: "traefik"},
		{Context: "hetzner-two", Type: ResourceService, Name: "traefik", Parent: "traefik"},
		{Context: "hetzner-two", Type: ResourceFileset, Name: "web_config"},
		{Context: "hetzner-two", Type: ResourceContainer, Name: "stale"},
	}

	if len(got) != len(want) {
		t.Fatalf("SeedRefs returned %d refs, want %d:\ngot  %+v\nwant %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ref %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Guards the ResourcePlan.AllResources trap: map iteration order must not leak
// into the seeded list.
func TestSeedRefsIsStableAcrossRuns(t *testing.T) {
	first := SeedRefs("ctx", fixtureResourcePlan())
	for i := 0; i < 50; i++ {
		again := SeedRefs("ctx", fixtureResourcePlan())
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("run %d differed at ref %d: %+v vs %+v", i, j, again[j], first[j])
			}
		}
	}
}

func TestSeedRefsNilPlan(t *testing.T) {
	if got := SeedRefs("ctx", nil); got != nil {
		t.Fatalf("SeedRefs(nil) = %+v, want nil", got)
	}
}

func TestApplyWithPlanSeedsBeforeWork(t *testing.T) {
	rec := &recordingReporter{}
	plan := &Plan{
		ByContext: map[string]*ContextPlan{
			"beta":  {ContextName: "beta", Resources: &ResourcePlan{Volumes: []Resource{NewResource(ResourceVolume, "b", ActionCreate, "")}}},
			"alpha": {ContextName: "alpha", Resources: &ResourcePlan{Volumes: []Resource{NewResource(ResourceVolume, "a", ActionCreate, "")}}},
		},
	}

	p := New().WithProgressReporter(rec)
	// Apply will fail (no docker client configured); we only assert that seeding
	// happened first, and that contexts were seeded in sorted order.
	_ = p.ApplyWithPlan(context.Background(), manifest.Config{}, plan)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.events) < 2 {
		t.Fatalf("expected at least 2 seed events, got %d", len(rec.events))
	}
	if rec.events[0].Kind != "seed" {
		t.Fatalf("first event was %q, want seed", rec.events[0].Kind)
	}
	if rec.events[0].Ref.Context != "alpha" || rec.events[1].Ref.Context != "beta" {
		t.Fatalf("contexts seeded out of order: %q then %q", rec.events[0].Ref.Context, rec.events[1].Ref.Context)
	}
}
