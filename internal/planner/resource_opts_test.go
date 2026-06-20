package planner

import (
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/ui"
)

func TestRenderResourcePlanOpts_ChangesOnly_FlatSections(t *testing.T) {
	rp := &ResourcePlan{
		Volumes: []Resource{
			NewResource(ResourceVolume, "vNew", ActionCreate, ""),
			NewResource(ResourceVolume, "vKeep1", ActionNoop, "exists"),
			NewResource(ResourceVolume, "vKeep2", ActionNoop, "exists"),
		},
		Networks: []Resource{
			NewResource(ResourceNetwork, "nKeep", ActionNoop, "exists"),
		},
	}
	out := ui.StripANSI(RenderResourcePlanOpts(rp, PlanRenderOptions{Full: false}))

	if !strings.Contains(out, "vNew") {
		t.Errorf("expected output to contain 'vNew', got:\n%s", out)
	}
	if strings.Contains(out, "vKeep1") {
		t.Errorf("expected output to NOT contain 'vKeep1' (no-op), got:\n%s", out)
	}
	if strings.Contains(out, "vKeep2") {
		t.Errorf("expected output to NOT contain 'vKeep2' (no-op), got:\n%s", out)
	}
	if !strings.Contains(out, "2 unchanged") {
		t.Errorf("expected Volumes footer '2 unchanged', got:\n%s", out)
	}
	if !strings.Contains(out, "1 unchanged") {
		t.Errorf("expected Networks footer '1 unchanged', got:\n%s", out)
	}
	if !strings.Contains(out, "Volumes") {
		t.Errorf("expected output to contain 'Volumes' header, got:\n%s", out)
	}
	if !strings.Contains(out, "Networks") {
		t.Errorf("expected output to contain 'Networks' header, got:\n%s", out)
	}
}

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
