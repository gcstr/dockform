package planner

import "strings"

// SectionOrder is the canonical top-to-bottom order of plan and apply sections.
// Both the plan renderer and the apply view read it, so the two cannot disagree
// about ordering.
var SectionOrder = []string{"Volumes", "Networks", "Stacks", "Filesets", "Containers"}

// SectionTitle maps a resource type to its section heading. Services share the
// Stacks section with the stack they belong to, and files share the Filesets
// section with their fileset, because both render nested under their parent.
//
// Exported so internal/ui/applyview consumes this definition rather than
// keeping its own copy, which is what allowed the two to drift.
func SectionTitle(t ResourceType) string {
	switch t {
	case ResourceVolume:
		return "Volumes"
	case ResourceNetwork:
		return "Networks"
	case ResourceStack, ResourceService:
		return "Stacks"
	case ResourceFileset, ResourceFile:
		return "Filesets"
	case ResourceContainer:
		return "Containers"
	default:
		return "Other"
	}
}

// FilesetDisplayName strips a fileset key's leading "context/" segment for
// display. Discovery keys filesets by "context/stack/volume" (see
// manifest.DiscoveredFilesets) so the key stays a unique identity across
// contexts, but both renderers already nest a fileset's section under a
// context header — printing the full key there just repeats it, e.g.
// "hetzner-two/traefik/config" under a "hetzner-two" header, while the
// sibling Stacks section shows a bare "traefik" (GetStacksForContext already
// strips the same leading segment for stack titles).
//
// Callers must keep using the full key as the map key and as
// ResourceRef.Name — this only affects what gets printed.
func FilesetDisplayName(key string) string {
	if idx := strings.IndexByte(key, '/'); idx >= 0 {
		return key[idx+1:]
	}
	return key
}
