package applyview

import "github.com/gcstr/dockform/internal/planner"

// stackResolves reports whether child resolves together with stack, which has
// just finished.
//
// The planner drives compose at stack granularity: one ComposeUp call per stack
// covers every service in it, so a stack finishing is the only signal its
// services will ever get. Both renderers have to agree on that rule — when only
// the interactive one implemented it, a successful stack's services were still
// reported "not applied" in CI, which is why this lives here rather than inside
// either renderer.
//
// Every field is compared deliberately: two hosts routinely run stacks with the
// same name, and one host routinely runs two stacks that share a service name.
func stackResolves(stack, child planner.ResourceRef) bool {
	return stack.Type == planner.ResourceStack &&
		child.Type == planner.ResourceService &&
		child.Context == stack.Context &&
		child.Parent == stack.Name
}

// countsTowardTotal reports whether ref should be counted in the resource
// totals both renderers report ("Applying N resources", "N of M resources
// applied"). A ResourceStack line is the same work as its services seen at a
// coarser grain — apply drives one compose call per stack, and stackResolves
// above is what makes a successful stack's services resolve with it — so
// counting the stack too reports more resources than the plan footer the
// user approved. This used to be reimplemented separately inside
// Model.Counts/Outcomes (which excluded it) and the plain renderer (which
// did not), so the same run printed a different total on a terminal than in
// CI; it lives here, alongside stackResolves, so both stay in sync.
func countsTowardTotal(ref planner.ResourceRef) bool {
	return ref.Type != planner.ResourceStack
}

// displayName returns ref.Name as it should be printed. A fileset's Name is
// keyed "context/stack/volume" for identity (see manifest.DiscoveredFilesets
// and planner.FilesetDisplayName), but every line in both renderers already
// shows its context once — as the group's context header here, as the
// qualifiedLabel prefix in Plain — so printing the full key repeats it, e.g.
// "hetzner-two/traefik/config" under a "hetzner-two" header. This lives here
// rather than in view.go or plain.go so both stay in sync, the same reason
// stackResolves does.
func displayName(ref planner.ResourceRef) string {
	if ref.Type == planner.ResourceFileset {
		return planner.FilesetDisplayName(ref.Name)
	}
	return ref.Name
}
