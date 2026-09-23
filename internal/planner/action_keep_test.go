package planner

import (
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/ui"
)

// keepPlan is a one-context destroy-style plan: one kept volume, one deleted.
func keepPlan() *Plan {
	rp := &ResourcePlan{
		Stacks:   map[string][]Resource{},
		Filesets: map[string][]Resource{},
		Volumes: []Resource{
			NewResource(ResourceVolume, "data", ActionKeep, ""),
			NewResource(ResourceVolume, "scratch", ActionDelete, "will be destroyed"),
		},
		Networks: []Resource{
			NewResource(ResourceNetwork, "keepnet", ActionKeep, ""),
		},
	}
	return &Plan{
		ByContext: map[string]*ContextPlan{"default": {ContextName: "default", Identifier: "demo", Resources: rp}},
		Resources: rp,
	}
}

func TestActionKeep_IsShownInFullAndChangesOnlyOutput(t *testing.T) {
	p := keepPlan()
	for name, out := range map[string]string{
		"full (what destroy prints)": p.String(),
		"changes-only":               p.Render(PlanRenderOptions{}),
	} {
		plain := ui.StripANSI(out)
		for _, want := range []string{"data kept (destroy: false)", "keepnet kept (destroy: false)"} {
			if !strings.Contains(plain, want) {
				t.Errorf("%s output lacks %q:\n%s", name, want, plain)
			}
		}
	}
}

func TestActionKeep_IsNotCountedAsADestroy(t *testing.T) {
	create, update, del := keepPlan().Resources.CountActions()
	if create != 0 || update != 0 || del != 1 {
		t.Errorf("CountActions = (%d, %d, %d), want (0, 0, 1): a kept resource is not a change", create, update, del)
	}
}

func TestActionKeep_IsNeverSeededAsPendingWork(t *testing.T) {
	for _, ref := range SeedRefs("default", keepPlan().Resources) {
		if ref.Name == "data" || ref.Name == "keepnet" {
			t.Errorf("a kept resource was seeded as pending work: %+v", ref)
		}
	}
}
