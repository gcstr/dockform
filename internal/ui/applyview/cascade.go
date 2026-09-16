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
