package planner

// SeedRefs returns the apply view's line list for one context, in the same order
// the plan renderer prints: volumes, networks, stacks (sorted, each followed by
// its changed services), filesets (sorted), then orphan containers. Resources
// that are not pending work (ActionNoop, ActionKeep — see Action.pending) are omitted:
// they must not occupy a line.
//
// Deliberately does NOT use ResourcePlan.AllResources (resource.go:483): that
// helper iterates the Stacks and Filesets maps unsorted, so its order varies
// between runs. This iterates SectionOrder (sections.go) instead, the same
// order the plan renderer reads, so the approved plan and the apply view read
// the same top to bottom.
//
// A fileset contributes exactly one ref regardless of how many files changed;
// per-file progress belongs on that line as a Detail.
func SeedRefs(contextName string, rp *ResourcePlan) []ResourceRef {
	if rp == nil {
		return nil
	}

	var refs []ResourceRef

	for _, title := range SectionOrder {
		switch title {
		case SectionTitle(ResourceVolume):
			for _, res := range rp.Volumes {
				if !res.Action.pending() {
					continue
				}
				refs = append(refs, ResourceRef{Context: contextName, Type: ResourceVolume, Name: res.Name})
			}

		case SectionTitle(ResourceNetwork):
			for _, res := range rp.Networks {
				if !res.Action.pending() {
					continue
				}
				refs = append(refs, ResourceRef{Context: contextName, Type: ResourceNetwork, Name: res.Name})
			}

		case SectionTitle(ResourceStack):
			for _, stackName := range sortedKeys(rp.Stacks) {
				var changed []Resource
				for _, svc := range rp.Stacks[stackName] {
					if !svc.Action.pending() {
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

		case SectionTitle(ResourceFileset):
			for _, fsName := range sortedKeys(rp.Filesets) {
				hasChange := false
				for _, item := range rp.Filesets[fsName] {
					if item.Action.pending() {
						hasChange = true
						break
					}
				}
				if !hasChange {
					continue
				}
				refs = append(refs, ResourceRef{Context: contextName, Type: ResourceFileset, Name: fsName})
			}

		case SectionTitle(ResourceContainer):
			for _, res := range rp.Containers {
				if !res.Action.pending() {
					continue
				}
				refs = append(refs, ResourceRef{Context: contextName, Type: ResourceContainer, Name: res.Name})
			}
		}
	}

	return refs
}
