package dashboardcmd

import (
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
)

func TestModelViewRendersColumnsAndPalette(t *testing.T) {
	m := newDashboardModel()
	m.width = 120
	m.height = 35
	if v := m.View(); v == "" {
		t.Fatalf("expected view output")
	}
	m.commandPaletteOpen = true
	m.commandList = newCommandPalette()
	if v := m.View(); v == "" {
		t.Fatalf("expected view with palette")
	}
}

func TestRenderSectionsAndHelpers(t *testing.T) {
	m := newDashboardModel()
	m.volumes = []dockercli.VolumeSummary{{Name: "vol1", Driver: "local", Mountpoint: "/data"}}
	m.networks = []dockercli.NetworkSummary{{Name: "net1", Driver: "bridge"}}
	m.containerVolumes = map[string][]string{"container": {"vol1"}}
	m.containerNetworks = map[string][]string{"container": {"net1"}}
	m.width = 100
	m.height = 30
	volSection := m.renderVolumesSection(40)
	if !strings.Contains(volSection, "vol1") {
		t.Fatalf("expected volume details in section")
	}
	netSection := m.renderNetworksSection(40)
	if !strings.Contains(netSection, "net1") {
		t.Fatalf("expected network details in section")
	}
	if displayNetworkDriver(" ") != "(driver unknown)" {
		t.Fatalf("expected network driver fallback")
	}
	if got := truncateRight("abcdefg", 4); got != "a..." {
		t.Fatalf("unexpected truncation result: %q", got)
	}
}
