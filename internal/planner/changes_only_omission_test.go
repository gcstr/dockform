package planner

import (
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/ui"
)

// changes-only output exists to show what will change. A context with nothing
// pending, or a section within it holding only no-ops, is noise: on a
// three-host manifest with one pending update it cost ~25 lines to say one.
func TestRenderPlanByContext_ChangesOnly_OmitsUnchangedContextsAndSections(t *testing.T) {
	byContext := map[string]*ContextPlan{
		"hetzner-one": {
			ContextName: "hetzner-one",
			Resources: &ResourcePlan{
				// Volumes: nothing pending -> the whole section goes.
				Volumes: []Resource{NewResource(ResourceVolume, "data", ActionNoop, "exists")},
				Stacks: map[string][]Resource{
					"wud": {
						NewResource(ResourceService, "whatsupdocker", ActionUpdate, ""),
						NewResource(ResourceService, "sidecar", ActionNoop, ""),
					},
				},
			},
		},
		// Nothing pending anywhere -> the whole context goes.
		"hetzner-two": {
			ContextName: "hetzner-two",
			Resources: &ResourcePlan{
				Volumes:  []Resource{NewResource(ResourceVolume, "cache", ActionNoop, "exists")},
				Networks: []Resource{NewResource(ResourceNetwork, "proxy", ActionNoop, "exists")},
				Stacks: map[string][]Resource{
					"idle": {NewResource(ResourceService, "idle", ActionNoop, "")},
				},
			},
		},
	}

	out := ui.StripANSI(renderPlanByContext(byContext, PlanRenderOptions{Full: false}))

	if !strings.Contains(out, "hetzner-one") {
		t.Errorf("context with a pending change must still render; got:\n%s", out)
	}
	if !strings.Contains(out, "whatsupdocker") {
		t.Errorf("the pending change must render; got:\n%s", out)
	}
	if strings.Contains(out, "hetzner-two") {
		t.Errorf("context with nothing pending must be omitted entirely; got:\n%s", out)
	}
	if strings.Contains(out, "Volumes") {
		t.Errorf("section with nothing pending must be omitted; got:\n%s", out)
	}
	if strings.Contains(out, "Networks") {
		t.Errorf("section with nothing pending must be omitted; got:\n%s", out)
	}
	// No "N unchanged" footers at all: changes-only shows what changes, and
	// --long is the escape hatch for the full inventory.
	if strings.Contains(out, "unchanged") {
		t.Errorf("changes-only must carry no unchanged footer; got:\n%s", out)
	}
}

// --long is the escape hatch: it must still show everything.
func TestRenderPlanByContext_Long_StillShowsUnchangedContexts(t *testing.T) {
	byContext := map[string]*ContextPlan{
		"hetzner-two": {
			ContextName: "hetzner-two",
			Resources: &ResourcePlan{
				Volumes: []Resource{NewResource(ResourceVolume, "cache", ActionNoop, "exists")},
			},
		},
	}

	out := ui.StripANSI(renderPlanByContext(byContext, PlanRenderOptions{Full: true}))

	if !strings.Contains(out, "hetzner-two") || !strings.Contains(out, "cache") {
		t.Errorf("--long must still show unchanged contexts and resources; got:\n%s", out)
	}
}
