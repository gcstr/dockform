package dashboardcmd

import (
	"testing"

	"github.com/gcstr/dockform/internal/cli/dashboardcmd/data"
)

func TestMergeAndUniqueHelpers(t *testing.T) {
	merged := mergeStringValues([]string{"a"}, []string{"b", "a"})
	if len(merged) != 2 {
		t.Fatalf("expected unique merged values, got %v", merged)
	}
	unique := uniqueSortedStrings([]string{"b", "a", "a"})
	if len(unique) != 2 || unique[0] != "a" || unique[1] != "b" {
		t.Fatalf("expected unique sorted strings, got %v", unique)
	}
}

func TestSelectedSetsAndContainerHelpers(t *testing.T) {
	m := newDashboardModel()
	m.containerVolumes = map[string][]string{"container": {"vol1"}, "svc": {"vol2"}}
	m.containerNetworks = map[string][]string{"container": {"net1"}}
	set := m.selectedVolumeSet()
	if _, ok := set["vol1"]; !ok {
		t.Fatalf("expected selected volume present")
	}
	netSet := m.selectedNetworkSet()
	if _, ok := netSet["net1"]; !ok {
		t.Fatalf("expected selected network present")
	}
	if m.selectedContainerName() == "" {
		t.Fatalf("expected container name to be returned")
	}
}

func TestModelTickCommands(t *testing.T) {
	m := newDashboardModel()
	_ = m.tickStatuses()
	_ = m.startInitialLogsCmd()
	m.statusProvider = data.NewStatusProvider(nil, "")
	_ = m.refreshStatusesCmd()
	_ = m.fetchDockerInfoCmd()
}
