package planner

import (
	"sort"
	"testing"
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
		name         string
		existing     map[string]struct{}
		desired      map[string]struct{}
		composeOwned map[string]struct{}
		want         []string
	}{
		{
			name:         "compose-owned network is never an orphan",
			existing:     set("app-net", "dockform-net"),
			desired:      set("dockform-net"),
			composeOwned: set("app-net"),
			want:         nil,
		},
		{
			name:         "dockform-managed network not desired is an orphan",
			existing:     set("dockform-net", "old-net"),
			desired:      set("dockform-net"),
			composeOwned: set(),
			want:         []string{"old-net"},
		},
		{
			name:         "compose-owned excluded but real orphan still removed",
			existing:     set("app-net", "old-net", "dockform-net"),
			desired:      set("dockform-net"),
			composeOwned: set("app-net"),
			want:         []string{"old-net"},
		},
		{
			name:         "all desired means no orphans",
			existing:     set("a", "b"),
			desired:      set("a", "b"),
			composeOwned: set(),
			want:         nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orphanNetworks(tt.existing, tt.desired, tt.composeOwned)
			sort.Strings(got)
			if len(got) != len(tt.want) {
				t.Fatalf("orphanNetworks = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("orphanNetworks = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
