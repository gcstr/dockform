package planner

// deleteKey identifies a planned deletion by the two fields prune can observe
// while removing a resource. Parent is deliberately absent: prune sees a compose
// project and service, not the stack name a seeded service ref carries.
type deleteKey struct {
	Type ResourceType
	Name string
}

// plannedDeletes indexes the refs the plan seeded for resources it wants removed,
// so prune can resolve the very lines the apply view is already showing.
//
// It reuses the seeded ref rather than building a fresh one at the removal site.
// ResourceRef compares on all four fields, so a reconstructed ref that differed in
// any of them — Parent especially, which prune has no way to know for an orphan
// service — would make the renderer append a phantom duplicate line instead of
// resolving the real one. The construction here mirrors SeedRefs; the two must
// agree, and TestPlannedDeletesAreSeeded pins that.
//
// A name appearing twice for one type (one host running two stacks that share a
// service name) is dropped rather than guessed at: resolving the wrong line is
// worse than leaving one pending.
func plannedDeletes(plan *Plan, contextName string) map[deleteKey]ResourceRef {
	if plan == nil {
		return nil
	}
	cp := plan.ByContext[contextName]
	if cp == nil || cp.Resources == nil {
		return nil
	}
	rp := cp.Resources

	out := map[deleteKey]ResourceRef{}
	ambiguous := map[deleteKey]struct{}{}

	add := func(k deleteKey, ref ResourceRef) {
		if _, dup := out[k]; dup {
			ambiguous[k] = struct{}{}
			return
		}
		out[k] = ref
	}

	for _, res := range rp.Volumes {
		if res.Action == ActionDelete {
			add(deleteKey{ResourceVolume, res.Name},
				ResourceRef{Context: contextName, Type: ResourceVolume, Name: res.Name})
		}
	}
	for _, res := range rp.Networks {
		if res.Action == ActionDelete {
			add(deleteKey{ResourceNetwork, res.Name},
				ResourceRef{Context: contextName, Type: ResourceNetwork, Name: res.Name})
		}
	}
	for _, stackName := range sortedKeys(rp.Stacks) {
		for _, svc := range rp.Stacks[stackName] {
			if svc.Action == ActionDelete {
				add(deleteKey{ResourceService, svc.Name},
					ResourceRef{Context: contextName, Type: ResourceService, Name: svc.Name, Parent: stackName})
			}
		}
	}
	for _, res := range rp.Containers {
		if res.Action == ActionDelete {
			add(deleteKey{ResourceContainer, res.Name},
				ResourceRef{Context: contextName, Type: ResourceContainer, Name: res.Name})
		}
	}

	for k := range ambiguous {
		delete(out, k)
	}
	return out
}

// reportRemoval runs remove, reporting it against the line the plan seeded for it.
// When the plan did not seed a line — prune removes things no plan enumerated —
// nothing is reported: inventing a line here would inflate the view's total after
// seeding, which the header promises not to do.
func reportRemoval(progress ProgressReporter, deletes map[deleteKey]ResourceRef, k deleteKey, remove func() error) error {
	ref, seeded := deletes[k]
	if !seeded {
		return remove()
	}
	progress.Start(ref, "removing")
	if err := remove(); err != nil {
		progress.Fail(ref, err)
		return err
	}
	progress.Finish(ref, "removed")
	return nil
}
