package dockercli

import (
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
)

// semCap returns the per-host concurrency cap of a client, or 0 when uncapped.
func semCap(t *testing.T, c *Client) int {
	t.Helper()
	se, ok := c.exec.(SystemExec)
	if !ok {
		t.Fatalf("client exec is %T, want SystemExec", c.exec)
	}
	return cap(se.sem)
}

func TestFactory_DefaultCapIsUnchanged(t *testing.T) {
	f := NewClientFactory()
	if got := semCap(t, f.GetClient("remote", "id")); got != MaxConcurrentSSH {
		t.Errorf("default remote cap = %d, want %d", got, MaxConcurrentSSH)
	}
}

func TestFactory_WithMaxConcurrentAppliesToEveryRemoteClient(t *testing.T) {
	f := NewClientFactory().WithMaxConcurrent(5)
	cfg := &manifest.Config{Identifier: "id", Contexts: map[string]manifest.ContextConfig{
		"named":  {},
		"tunnel": {Host: "unix:///tmp/dftun-1/0.tunnel.sock"},
		"ssh":    {Host: "ssh://u@h"},
	}}
	for _, name := range []string{"named", "tunnel", "ssh"} {
		if got := semCap(t, f.GetClientForContext(name, cfg)); got != 5 {
			t.Errorf("%s: cap = %d, want 5", name, got)
		}
	}
}

// The local daemon was never capped; a remote-host limit must not start capping it.
func TestFactory_WithMaxConcurrentLeavesLocalUncapped(t *testing.T) {
	f := NewClientFactory().WithMaxConcurrent(5)
	if got := semCap(t, f.GetClient("default", "id")); got != 0 {
		t.Errorf("local default context cap = %d, want uncapped (0)", got)
	}
}
