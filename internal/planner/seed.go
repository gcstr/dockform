package planner

// SeedRefs returns the apply view's line list for one context, in the same order
// the plan renderer prints: volumes, networks, stacks (sorted, each followed by
// its changed services), filesets (sorted), then orphan containers. Resources
// whose action is ActionNoop are omitted — they are not work and must not occupy
// a line.
//
// Deliberately does NOT use ResourcePlan.AllResources (resource.go:483): that
// helper iterates the Stacks and Filesets maps unsorted, so its order varies
// between runs. This mirrors renderResourcePlanFull (resource.go:138) instead,
// so the approved plan and the apply view read the same top to bottom.
//
// A fileset contributes exactly one ref regardless of how many files changed;
// per-file progress belongs on that line as a Detail.
func SeedRefs(contextName string, rp *ResourcePlan) []ResourceRef {
	if rp == nil {
		return nil
	}

	var refs []ResourceRef

	for _, res := range rp.Volumes {
		if res.Action == ActionNoop {
			continue
		}
		refs = append(refs, ResourceRef{Context: contextName, Type: ResourceVolume, Name: res.Name})
	}

	for _, res := range rp.Networks {
		if res.Action == ActionNoop {
			continue
		}
		refs = append(refs, ResourceRef{Context: contextName, Type: ResourceNetwork, Name: res.Name})
	}

	for _, stackName := range sortedKeys(rp.Stacks) {
		var changed []Resource
		for _, svc := range rp.Stacks[stackName] {
			if svc.Action == ActionNoop {
				continue
			}
			changed = append(changed, svc)
		}
		if len(changed) == 0 {
			continue
		}
		refs = append(refs, ResourceRef{Context: contextName, Type: ResourceStack, Name: stackName})
		for _, svc := range changed {
			refs = append(refs, ResourceRef{
				Context: contextName,
				Type:    ResourceService,
				Name:    svc.Name,
				Parent:  stackName,
			})
		}
	}

	for _, fsName := range sortedKeys(rp.Filesets) {
		hasChange := false
		for _, item := range rp.Filesets[fsName] {
			if item.Action != ActionNoop {
				hasChange = true
				break
			}
		}
		if !hasChange {
			continue
		}
		refs = append(refs, ResourceRef{Context: contextName, Type: ResourceFileset, Name: fsName})
	}

	for _, res := range rp.Containers {
		if res.Action == ActionNoop {
			continue
		}
		refs = append(refs, ResourceRef{Context: contextName, Type: ResourceContainer, Name: res.Name})
	}

	return refs
}
