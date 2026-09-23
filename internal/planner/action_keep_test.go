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

func TestHasDeletes(t *testing.T) {
	cases := []struct {
		name string
		rp   *ResourcePlan
		want bool
	}{
		{
			name: "fileset delete with empty Name is still a delete",
			rp: &ResourcePlan{
				Filesets: map[string][]Resource{
					"site": {NewResource(ResourceFile, "", ActionDelete, "will be destroyed")},
				},
			},
			want: true,
		},
		{
			name: "only kept volumes and networks",
			rp: &ResourcePlan{
				Volumes:  []Resource{NewResource(ResourceVolume, "data", ActionKeep, "")},
				Networks: []Resource{NewResource(ResourceNetwork, "keepnet", ActionKeep, "")},
			},
			want: false,
		},
		{
			name: "only no-ops",
			rp: &ResourcePlan{
				Volumes: []Resource{NewResource(ResourceVolume, "data", ActionNoop, "")},
			},
			want: false,
		},
		{
			name: "a stack service delete",
			rp: &ResourcePlan{
				Stacks: map[string][]Resource{
					"web": {NewResource(ResourceService, "app", ActionDelete, "will be destroyed")},
				},
			},
			want: true,
		},
		{
			name: "nil plan",
			rp:   nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rp.HasDeletes(); got != tc.want {
				t.Errorf("HasDeletes() = %v, want %v", got, tc.want)
			}
		})
	}
}
