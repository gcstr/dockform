package planner

import (
	"fmt"
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

func TestRenderResourcePlanOpts_ChangesOnly_Stacks(t *testing.T) {
	rp := &ResourcePlan{Stacks: map[string][]Resource{
		"ctx/app": {
			NewResource(ResourceService, "web", ActionCreate, ""),
			NewResource(ResourceService, "db", ActionNoop, "up-to-date"),
		},
		"ctx/idle": {
			NewResource(ResourceService, "x", ActionNoop, "up-to-date"),
			NewResource(ResourceService, "y", ActionNoop, "up-to-date"),
		},
	}}
	out := ui.StripANSI(RenderResourcePlanOpts(rp, PlanRenderOptions{Full: false}))

	if !strings.Contains(out, "ctx/app") {
		t.Errorf("expected output to contain 'ctx/app', got:\n%s", out)
	}
	if !strings.Contains(out, "web") {
		t.Errorf("expected output to contain 'web', got:\n%s", out)
	}
	if strings.Contains(out, "ctx/idle") {
		t.Errorf("expected output to NOT contain 'ctx/idle' (all no-op), got:\n%s", out)
	}
	if strings.Contains(out, "  x") || strings.Contains(out, "  y") {
		t.Errorf("expected output to NOT contain noop service lines 'x'/'y', got:\n%s", out)
	}
	if strings.Contains(out, " db ") {
		t.Errorf("expected output to NOT contain noop service 'db', got:\n%s", out)
	}
	if !strings.Contains(out, "3 unchanged") {
		t.Errorf("expected footer '3 unchanged' (db+x+y), got:\n%s", out)
	}
	if !strings.Contains(out, "Stacks") {
		t.Errorf("expected output to contain 'Stacks' header, got:\n%s", out)
	}
}

func TestRenderResourcePlanOpts_ChangesOnly_FilesetsCount(t *testing.T) {
	rp := &ResourcePlan{Filesets: map[string][]Resource{
		"ctx/a/cfg":  {{Type: ResourceFile, Name: "", Action: ActionNoop, Details: "no file changes", ChangeType: ui.Noop}},
		"ctx/a/data": {{Type: ResourceFile, Name: "f1", Action: ActionUpdate, Details: "", ChangeType: ui.Change}},
	}}
	out := ui.StripANSI(RenderResourcePlanOpts(rp, PlanRenderOptions{Full: false}))

	if !strings.Contains(out, "ctx/a/data") {
		t.Errorf("expected output to contain 'ctx/a/data', got:\n%s", out)
	}
	if !strings.Contains(out, "f1") {
		t.Errorf("expected output to contain 'f1', got:\n%s", out)
	}
	if strings.Contains(out, "ctx/a/cfg") {
		t.Errorf("expected output to NOT contain 'ctx/a/cfg' (fully unchanged), got:\n%s", out)
	}
	if !strings.Contains(out, "1 unchanged") {
		t.Errorf("expected Filesets footer '1 unchanged', got:\n%s", out)
	}
}

func TestRenderResourcePlanOpts_ChangesOnly_FilesetCap(t *testing.T) {
	// 13 changed files, all ActionUpdate
	items := make([]Resource, 13)
	for i := range items {
		items[i] = NewResource(ResourceFile, fmt.Sprintf("f%02d", i+1), ActionUpdate, "")
	}
	rp := &ResourcePlan{Filesets: map[string][]Resource{
		"ctx/a/big": items,
	}}

	// Changes-only: only 10 shown, remainder summarised
	out := ui.StripANSI(RenderResourcePlanOpts(rp, PlanRenderOptions{Full: false}))
	if strings.Contains(out, "f11") {
		t.Errorf("expected 'f11' to be suppressed (beyond cap of 10), got:\n%s", out)
	}
	if !strings.Contains(out, "… and 3 more changed (0 created, 3 updated, 0 deleted)") {
		t.Errorf("expected remainder summary line, got:\n%s", out)
	}

	// Full: all 13 shown, no "more changed" line
	outFull := ui.StripANSI(RenderResourcePlanOpts(rp, PlanRenderOptions{Full: true}))
	if !strings.Contains(outFull, "f11") {
		t.Errorf("expected full render to show 'f11', got:\n%s", outFull)
	}
	if strings.Contains(outFull, "more changed") {
		t.Errorf("expected full render to NOT contain 'more changed', got:\n%s", outFull)
	}
}

func TestRenderResourcePlanOpts_ChangesOnly_AllStacksUnchanged(t *testing.T) {
	rp := &ResourcePlan{
		Volumes: []Resource{NewResource(ResourceVolume, "vNew", ActionCreate, "")},
		Stacks: map[string][]Resource{
			"ctx/app": {
				NewResource(ResourceService, "web", ActionNoop, "up-to-date"),
				NewResource(ResourceService, "db", ActionNoop, "up-to-date"),
			},
		},
	}
	out := ui.StripANSI(RenderResourcePlanOpts(rp, PlanRenderOptions{Full: false}))

	if !strings.Contains(out, "Stacks") {
		t.Errorf("expected output to contain 'Stacks' header, got:\n%s", out)
	}
	if !strings.Contains(out, "2 unchanged") {
		t.Errorf("expected footer '2 unchanged', got:\n%s", out)
	}
	if strings.Contains(out, "ctx/app") {
		t.Errorf("expected output to NOT contain 'ctx/app' subsection (no changed services), got:\n%s", out)
	}
	if strings.Contains(out, "web") {
		t.Errorf("expected output to NOT contain 'web' service line, got:\n%s", out)
	}
	if strings.Contains(out, "db") {
		t.Errorf("expected output to NOT contain 'db' service line, got:\n%s", out)
	}
	if !strings.Contains(out, "vNew") {
		t.Errorf("expected output to contain 'vNew' (volume still rendered), got:\n%s", out)
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
