package planner

import (
	"reflect"
	"sort"
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
)

func set(names ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}

func TestOrphanNetworks(t *testing.T) {
	tests := []struct {
		name            string
		existing        map[string]struct{}
		desired         map[string]struct{}
		composeOwned    map[string]string // network -> owning compose project
		desiredProjects map[string]struct{}
		want            []string
	}{
		{
			name:            "compose-owned network of an active stack is not an orphan",
			existing:        set("app-net", "dockform-net"),
			desired:         set("dockform-net"),
			composeOwned:    map[string]string{"app-net": "app"},
			desiredProjects: set("app"),
			want:            nil,
		},
		{
			name:            "dockform-managed network not desired is an orphan",
			existing:        set("dockform-net", "old-net"),
			desired:         set("dockform-net"),
			composeOwned:    map[string]string{},
			desiredProjects: set(),
			want:            []string{"old-net"},
		},
		{
			name:            "active compose network kept, real orphan still removed",
			existing:        set("app-net", "old-net", "dockform-net"),
			desired:         set("dockform-net"),
			composeOwned:    map[string]string{"app-net": "app"},
			desiredProjects: set("app"),
			want:            []string{"old-net"},
		},
		{
			name:            "all desired means no orphans",
			existing:        set("a", "b"),
			desired:         set("a", "b"),
			composeOwned:    map[string]string{},
			desiredProjects: set(),
			want:            nil,
		},
		{
			name:            "compose network of a removed stack is an orphan",
			existing:        set("app_default", "gone_default"),
			desired:         set(),
			composeOwned:    map[string]string{"app_default": "app", "gone_default": "gone"},
			desiredProjects: set("app"),
			want:            []string{"gone_default"},
		},
		{
			name:            "without project information compose networks are kept",
			existing:        set("gone_default"),
			desired:         set(),
			composeOwned:    map[string]string{"gone_default": "gone"},
			desiredProjects: nil,
			want:            nil,
		},
		{
			name:            "a compose network with no project label is kept",
			existing:        set("mystery"),
			desired:         set(),
			composeOwned:    map[string]string{"mystery": ""},
			desiredProjects: set("app"),
			want:            nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orphanNetworks(tt.existing, tt.desired, tt.composeOwned, tt.desiredProjects)
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
				t.Fatalf("orphanNetworks = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeComposeProject(t *testing.T) {
	cases := map[string]string{
		"zz-probe-a": "zz-probe-a",
		"Zz-Probe-A": "zz-probe-a",
		"My.App_1":   "myapp_1",
		"__lead":     "lead",
		"":           "",
	}
	for in, want := range cases {
		if got := normalizeComposeProject(in); got != want {
			t.Errorf("normalizeComposeProject(%q) = %q, want %q", in, got, want)
		}
	}
}

// A stack without an explicit project name is matched on both its stack name and
// its directory name: compose defaults the project to the directory, while
// dockform's destroy scoping uses the stack name. Accepting both only ever keeps
// more networks, never deletes an active one.
func TestDesiredComposeProjects(t *testing.T) {
	got := desiredComposeProjects(map[string]manifest.Stack{
		"hetzner-two/MyApp":  {RootAbs: "/srv/homelab/hetzner-two/MyApp"},
		"hetzner-two/web":    {RootAbs: "/srv/website"},
		"hetzner-two/custom": {RootAbs: "/srv/custom", Project: &manifest.Project{Name: "Custom.Name"}},
	})
	want := set("myapp", "web", "website", "customname")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("desiredComposeProjects = %v, want %v", got, want)
	}
}
