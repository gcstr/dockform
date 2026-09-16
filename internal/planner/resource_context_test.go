package planner

import (
	"strings"
	"testing"
)

// Two contexts each own a volume with the SAME name. Before context grouping
// these rendered as two identical lines with no way to tell them apart.
func TestRenderPlanByContext_DisambiguatesSameNamedVolumes(t *testing.T) {
	byContext := map[string]*ContextPlan{
		"hetzner-three": {
			ContextName: "hetzner-three",
			Resources: &ResourcePlan{
				Volumes: []Resource{NewResource(ResourceVolume, "beszel_agent_data", ActionCreate, "")},
			},
		},
		"hetzner-one": {
			ContextName: "hetzner-one",
			Resources: &ResourcePlan{
				Volumes: []Resource{NewResource(ResourceVolume, "beszel_agent_data", ActionCreate, "")},
			},
		},
	}

	out := renderPlanByContext(byContext, PlanRenderOptions{Full: true})

	if !strings.Contains(out, "hetzner-one") || !strings.Contains(out, "hetzner-three") {
		t.Fatalf("both context headers must appear; got:\n%s", out)
	}
	// Sorted: hetzner-one before hetzner-three.
	if strings.Index(out, "hetzner-one") > strings.Index(out, "hetzner-three") {
		t.Errorf("contexts must render in sorted order; got:\n%s", out)
	}
	if got := strings.Count(out, "beszel_agent_data"); got != 2 {
		t.Errorf("want the volume once per context (2); got %d\n%s", got, out)
	}
}

// A single context still gets a header — one shape for everyone.
func TestRenderPlanByContext_SingleContextStillShowsHeader(t *testing.T) {
	byContext := map[string]*ContextPlan{
		"default": {
			ContextName: "default",
			Resources: &ResourcePlan{
				Volumes: []Resource{NewResource(ResourceVolume, "app_data", ActionCreate, "")},
			},
		},
	}

	out := renderPlanByContext(byContext, PlanRenderOptions{Full: true})

	if !strings.Contains(out, "default") {
		t.Errorf("single context must still render its header; got:\n%s", out)
	}
}

// A fileset section title must not repeat the context header it nests under.
// The map key is "context/stack/volume" for identity, but the displayed title
// should read "traefik/config" (matching the bare stack title GetStacksForContext
// already produces), not "hetzner-two/traefik/config".
func TestRenderPlanByContext_FilesetTitleOmitsContext(t *testing.T) {
	byContext := map[string]*ContextPlan{
		"hetzner-two": {
			ContextName: "hetzner-two",
			Resources: &ResourcePlan{
				Filesets: map[string][]Resource{
					"hetzner-two/traefik/config": {
						NewNestedResource(ResourceFile, "traefik.yml", "hetzner-two/traefik/config", ActionCreate, ""),
					},
				},
			},
		},
	}

	out := renderPlanByContext(byContext, PlanRenderOptions{Full: true})

	if strings.Contains(out, "hetzner-two/traefik/config") {
		t.Errorf("fileset title must not repeat the context prefix; got:\n%s", out)
	}
	if !strings.Contains(out, "traefik/config") {
		t.Errorf("fileset title must show the stack/volume portion; got:\n%s", out)
	}
}
