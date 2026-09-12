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

// TestApplyWithPlanSeedsBeforeWork proves the two properties ApplyWithPlan's
// seeding step must uphold (see apply.go around the reporter.Seed(refs) call):
//
//  1. Seed is called EXACTLY ONCE, with every context's refs concatenated,
//     not once per context. A regression to seeding per-context would still
//     produce seed-kind events first, so a count on rec.seedCalls is the only
//     way to catch it.
//  2. Seeding happens before any Start/Detail/Finish/Fail event. To make that
//     meaningful, this test gives the config real work (a volume to create)
//     backed by a mock docker client, so applyContext genuinely runs and
//     emits at least one non-seed event to order against.
func TestApplyWithPlanSeedsBeforeWork(t *testing.T) {
	rec := &recordingReporter{}
	plan := &Plan{
		ByContext: map[string]*ContextPlan{
			"beta":  {ContextName: "beta", Resources: &ResourcePlan{Volumes: []Resource{NewResource(ResourceVolume, "b", ActionCreate, "")}}},
			"alpha": {ContextName: "alpha", Resources: &ResourcePlan{Volumes: []Resource{NewResource(ResourceVolume, "a", ActionCreate, "")}}},
		},
	}

	cfg := manifest.Config{
		Identifier: "test-id",
		Contexts: map[string]manifest.ContextConfig{
			// Sequential (WithParallel(false) below) so the two contexts'
			// work events cannot interleave in the shared mock docker client;
			// the property under test (seed precedes all work) does not
			// depend on parallelism, so there is nothing to gain by racing.
			"alpha": {Volumes: map[string]manifest.TopLevelResourceSpec{"a": {}}},
			"beta":  {Volumes: map[string]manifest.TopLevelResourceSpec{"b": {}}},
		},
	}

	mock := newMockDocker()
	p := NewWithDocker(mock).WithProgressReporter(rec).WithParallel(false)
	if err := p.ApplyWithPlan(context.Background(), cfg, plan); err != nil {
		t.Fatalf("ApplyWithPlan: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	// Property 1: exactly one Seed call.
	if rec.seedCalls != 1 {
		t.Fatalf("Seed was called %d times, want exactly 1 (one call for every context, not one call per context)", rec.seedCalls)
	}

	if len(rec.events) < 2 {
		t.Fatalf("expected at least 2 seed events, got %d", len(rec.events))
	}
	if rec.events[0].Kind != "seed" {
		t.Fatalf("first event was %q, want seed", rec.events[0].Kind)
	}
	if rec.events[0].Ref.Context != "alpha" || rec.events[1].Ref.Context != "beta" {
		t.Fatalf("contexts seeded out of order: %q then %q", rec.events[0].Ref.Context, rec.events[1].Ref.Context)
	}

	// Property 2: seeding precedes all real work. Confirm real work actually
	// happened (otherwise "precedes no events" would trivially pass), then
	// check every seed event's index precedes every non-seed event's index.
	lastSeedIdx := -1
	firstWorkIdx := -1
	for i, e := range rec.events {
		if e.Kind == "seed" {
			lastSeedIdx = i
		} else if firstWorkIdx == -1 {
			firstWorkIdx = i
		}
	}
	if firstWorkIdx == -1 {
		t.Fatalf("expected at least one non-seed (Start/Finish/Fail) event from real work, got none: %+v", rec.events)
	}
	if firstWorkIdx < lastSeedIdx {
		t.Fatalf("a non-seed event at index %d arrived before the last seed event at index %d: %+v", firstWorkIdx, lastSeedIdx, rec.events)
	}

	if len(mock.createdVolumes) != 2 {
		t.Fatalf("expected both volumes to be created by real work, got %v", mock.createdVolumes)
	}
}

// TestApplyWithPlanNilContextPlanDoesNotPanic guards the cp == nil check in
// ApplyWithPlan's seeding loop (apply.go): without it, a nil *ContextPlan's
// Resources field is read through a nil pointer before SeedRefs is ever
// called, which panics. plan.ByContext should never legitimately contain a
// nil entry, but the guard exists to fail safe rather than crash if it does.
func TestApplyWithPlanNilContextPlanDoesNotPanic(t *testing.T) {
	rec := &recordingReporter{}
	plan := &Plan{
		ByContext: map[string]*ContextPlan{
			"alpha": nil,
		},
	}

	p := New().WithProgressReporter(rec)
	_ = p.ApplyWithPlan(context.Background(), manifest.Config{}, plan)
}
