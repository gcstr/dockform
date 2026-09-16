package planner

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
