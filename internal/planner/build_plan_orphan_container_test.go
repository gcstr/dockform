package planner

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

// A removed stack whose service name matches an active stack's service must show
// up for deletion in the plan: containers are matched on project and service.
func TestBuildPlan_OrphanContainerWithCollidingServiceName(t *testing.T) {
	mock := newMockDocker() // a root named "website" resolves to project website, service nginx
	mock.containers = []dockercli.PsBrief{
		{Name: "website-nginx-1", Project: "website", Service: "nginx"},
		{Name: "gone-nginx-1", Project: "gone", Service: "nginx"},
	}
	cfg := manifest.Config{
		Identifier: "test",
		Contexts:   map[string]manifest.ContextConfig{"default": {}},
		Stacks:     map[string]manifest.Stack{"default/website": {Root: filepath.Join(t.TempDir(), "website"), Files: []string{"compose.yml"}}},
	}

	plan, err := NewWithDocker(mock).BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}
	stacks := plan.ByContext["default"].Resources.Stacks

	deletes := func(stack string) []string {
		var out []string
		for _, r := range stacks[stack] {
			if r.Action == ActionDelete {
				out = append(out, r.Name)
			}
		}
		return out
	}
	if got := deletes("gone"); len(got) != 1 || got[0] != "nginx" {
		t.Errorf("deletes for removed stack gone = %v, want [nginx]", got)
	}
	if got := deletes("website"); len(got) != 0 {
		t.Errorf("deletes for active stack website = %v, want none", got)
	}
}
