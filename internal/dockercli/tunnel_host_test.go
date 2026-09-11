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
