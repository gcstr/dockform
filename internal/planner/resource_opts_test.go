package planner

import (
	"testing"
)

func TestRenderResourcePlanOpts_FullMatchesLegacy(t *testing.T) {
	rp := &ResourcePlan{
		Volumes: []Resource{
			NewResource(ResourceVolume, "vNew", ActionCreate, ""),
			NewResource(ResourceVolume, "vKeep", ActionNoop, "exists"),
		},
		Networks: []Resource{
			NewResource(ResourceNetwork, "nKeep", ActionNoop, "exists"),
		},
		Stacks: map[string][]Resource{
			"ctx/app": {
				NewResource(ResourceService, "web", ActionCreate, ""),
				NewResource(ResourceService, "db", ActionNoop, "up-to-date"),
			},
		},
		Filesets: map[string][]Resource{
			"ctx/app/config": {
				NewResource(ResourceFile, "", ActionNoop, "no file changes"),
			},
		},
	}

	legacy := RenderResourcePlan(rp)
	opts := RenderResourcePlanOpts(rp, PlanRenderOptions{Full: true})

	if legacy != opts {
		t.Errorf("RenderResourcePlanOpts(Full=true) output differs from RenderResourcePlan\nlegacy:\n%s\nopts:\n%s", legacy, opts)
	}
}
