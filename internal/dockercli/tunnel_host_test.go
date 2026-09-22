package dockercli

import (
	"testing"

	"github.com/gcstr/dockform/internal/sshtunnel"
)

// A tunnel socket is local on disk but the daemon behind it is remote, so the
// per-host concurrency cap must still apply — even for a context named default.
func TestIsRemoteContext_TunnelSocketCountsAsRemote(t *testing.T) {
	if !isRemoteContext("default", "unix:///tmp/dftun-ab12/0"+sshtunnel.SocketSuffix) {
		t.Error("a tunnel socket must count as a remote context")
	}
	if isRemoteContext("default", "unix:///var/run/docker.sock") {
		t.Error("the local docker socket must not count as remote")
	}
}

// IsRemote applies the same rule as the per-host --parallel cap, so the two
// can never disagree about which daemons are remote.
func TestClientIsRemote(t *testing.T) {
	cases := []struct {
		name   string
		client *Client
		want   bool
	}{
		{"default context", New("default"), false},
		{"named context", New("hetzner-one"), true},
		{"ssh host override", NewWithHost("default", "ssh://user@host"), true},
		{"local socket override", NewWithHost("default", "unix:///var/run/docker.sock"), false},
	}
	for _, tc := range cases {
		if got := tc.client.IsRemote(); got != tc.want {
			t.Errorf("%s: IsRemote() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
