package planner

import "testing"

// plannedDeletes reconstructs the refs SeedRefs produced. If the two ever drift,
// prune resolves a ref the view never seeded and the renderer appends a phantom
// duplicate line instead of clearing the pending one — the exact bug this bridge
// exists to fix. This test fails the moment either side changes alone.
func TestPlannedDeletesAreSeeded(t *testing.T) {
	rp := &ResourcePlan{
		Volumes: []Resource{
			NewResource(ResourceVolume, "orphan-vol", ActionDelete, ""),
			NewResource(ResourceVolume, "keep-vol", ActionCreate, ""),
		},
		Networks: []Resource{
			NewResource(ResourceNetwork, "orphan-net", ActionDelete, ""),
		},
		Stacks: map[string][]Resource{
			"web": {
				NewNestedResource(ResourceService, "gone", "web", ActionDelete, ""),
				NewNestedResource(ResourceService, "stays", "web", ActionCreate, ""),
			},
		},
		Containers: []Resource{
			NewResource(ResourceContainer, "stale", ActionDelete, ""),
		},
	}
	plan := &Plan{ByContext: map[string]*ContextPlan{"ctx": {ContextName: "ctx", Resources: rp}}}

	seeded := map[ResourceRef]struct{}{}
	for _, ref := range SeedRefs("ctx", rp) {
		seeded[ref] = struct{}{}
	}

	deletes := plannedDeletes(plan, "ctx")
	if len(deletes) != 4 {
		t.Fatalf("plannedDeletes returned %d refs, want 4: %+v", len(deletes), deletes)
	}
	for key, ref := range deletes {
		if _, ok := seeded[ref]; !ok {
			t.Errorf("plannedDeletes[%+v] = %+v, which SeedRefs never seeded; prune would resolve a phantom line", key, ref)
		}
	}
}

// Two stacks on one host sharing a service name cannot be told apart from the
// compose project/service pair prune observes, so the entry is dropped rather
// than guessed at: resolving the wrong line is worse than leaving one pending.
func TestPlannedDeletesDropsAmbiguousNames(t *testing.T) {
	rp := &ResourcePlan{
		Stacks: map[string][]Resource{
			"alpha": {NewNestedResource(ResourceService, "web", "alpha", ActionDelete, "")},
			"beta":  {NewNestedResource(ResourceService, "web", "beta", ActionDelete, "")},
		},
	}
	plan := &Plan{ByContext: map[string]*ContextPlan{"ctx": {ContextName: "ctx", Resources: rp}}}

	if got := plannedDeletes(plan, "ctx"); len(got) != 0 {
		t.Fatalf("ambiguous service name should be dropped, got %+v", got)
	}
}

func TestPlannedDeletesNilPlan(t *testing.T) {
	if got := plannedDeletes(nil, "ctx"); got != nil {
		t.Fatalf("plannedDeletes(nil) = %+v, want nil", got)
	}
	empty := &Plan{ByContext: map[string]*ContextPlan{}}
	if got := plannedDeletes(empty, "missing"); got != nil {
		t.Fatalf("plannedDeletes for an absent context = %+v, want nil", got)
	}
}
