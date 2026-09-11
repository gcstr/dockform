package planner

import (
	"context"
	"errors"
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

// The project comes from the manifest when set (dockform passes it as -p) and
// otherwise from compose itself, which honors COMPOSE_PROJECT_NAME and name:.
func TestStackComposeProject(t *testing.T) {
	mock := newMockDocker()
	mock.composeProjectNames = map[string]string{"/srv/named": "custom-project", "/srv/blank": ""}

	cases := []struct {
		name    string
		stack   manifest.Stack
		want    string
		wantErr bool
	}{
		{"explicit manifest project wins", manifest.Stack{Root: "/srv/named", Project: &manifest.Project{Name: "Explicit"}}, "explicit", false},
		{"name resolved by compose", manifest.Stack{Root: "/srv/named"}, "custom-project", false},
		{"compose default is the directory", manifest.Stack{Root: "/srv/My.App"}, "myapp", false},
		{"empty resolved name is an error", manifest.Stack{Root: "/srv/blank"}, "", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stackComposeProject(context.Background(), mock, tt.stack, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("project = %q, want %q", got, tt.want)
			}
		})
	}

	mock.composeConfigFullError = errors.New("compose config failed")
	if _, err := stackComposeProject(context.Background(), mock, manifest.Stack{Root: "/srv/other"}, nil); err == nil {
		t.Fatal("expected an error when compose config fails")
	}
}
