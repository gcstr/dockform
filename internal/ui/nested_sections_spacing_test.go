package ui

import (
	"strings"
	"testing"
)

// TestRenderNestedSections_NoBlankLineBetweenNestedSiblings mirrors the shape
// destroy.go renders directly (RenderResourcePlanOpts -> RenderNestedSections,
// with no per-context wrapping): a "Stacks" section containing two nested
// per-stack subsections. Only RenderNestedSections' own top-level siblings
// get blank-line separation; anything nested must stay tight, exactly as it
// did before per-context grouping existed. A regression here would
// double-space `dockform destroy`'s multi-stack/multi-fileset review output.
func TestRenderNestedSections_NoBlankLineBetweenNestedSiblings(t *testing.T) {
	sections := []NestedSection{
		{
			Title: "Stacks",
			Sections: []NestedSection{
				{Title: "app", Items: []DiffLine{{Type: Add, Message: "web will be created"}}},
				{Title: "proj", Items: []DiffLine{{Type: Remove, Message: "other will be deleted"}}},
			},
		},
	}

	out := StripANSI(RenderNestedSections(sections))

	appIdx := strings.Index(out, "app")
	projIdx := strings.Index(out, "proj")
	if appIdx == -1 || projIdx == -1 {
		t.Fatalf("expected both nested section headers, got:\n%q", out)
	}
	if between := out[appIdx:projIdx]; strings.Contains(between, "\n\n") {
		t.Errorf("expected no blank line between nested sibling sections 'app' and 'proj', got:\n%q", out)
	}
}

// TestRenderGroupedNestedSections_SpacesWrappedResourceSections mirrors the
// per-context grouped shape (renderPlanByContext): a context section wraps
// the existing Volumes/Stacks tree one level deeper. The wrapped
// resource-type sections (Volumes, Stacks) must keep the blank-line
// separation they had before being wrapped; the per-stack subsections
// beneath Stacks must still render tight against each other, unaffected by
// the extra wrapping level.
func TestRenderGroupedNestedSections_SpacesWrappedResourceSections(t *testing.T) {
	sections := []NestedSection{
		{
			Title: "default",
			Sections: []NestedSection{
				{Title: "Volumes", Items: []DiffLine{{Type: Add, Message: "v1 will be created"}}},
				{
					Title: "Stacks",
					Sections: []NestedSection{
						{Title: "app", Items: []DiffLine{{Type: Add, Message: "web will be created"}}},
						{Title: "proj", Items: []DiffLine{{Type: Remove, Message: "other will be deleted"}}},
					},
				},
			},
		},
	}

	out := StripANSI(RenderGroupedNestedSections(sections))

	volIdx := strings.Index(out, "Volumes")
	stacksIdx := strings.Index(out, "Stacks")
	if volIdx == -1 || stacksIdx == -1 {
		t.Fatalf("expected both Volumes and Stacks headers, got:\n%s", out)
	}
	if between := out[volIdx:stacksIdx]; !strings.Contains(between, "\n\n") {
		t.Errorf("expected a blank line between the wrapped Volumes and Stacks sections, got:\n%q", out)
	}

	appIdx := strings.Index(out, "app")
	projIdx := strings.Index(out, "proj")
	if appIdx == -1 || projIdx == -1 {
		t.Fatalf("expected both per-stack headers, got:\n%s", out)
	}
	if between := out[appIdx:projIdx]; strings.Contains(between, "\n\n") {
		t.Errorf("expected no blank line between the per-stack subsections 'app' and 'proj', got:\n%q", out)
	}
}
