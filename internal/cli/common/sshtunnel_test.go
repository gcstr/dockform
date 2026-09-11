package common

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/sshtunnel"
	"github.com/spf13/cobra"
)

type fakeTunnel struct {
	ln     net.Listener
	stderr string
}

func (f *fakeTunnel) Stderr() string { return f.stderr }
func (f *fakeTunnel) Close()         { _ = f.ln.Close() }

type openCall struct{ dest, remote string }

// stubTunnelDeps replaces the ssh/docker-touching hooks for one test.
func stubTunnelDeps(t *testing.T, endpoints map[string]string, openErr map[string]error, stderrFor func(remote string) string,
	check func(ctx context.Context, name, local string) error, remoteSocket string) *[]openCall {
	t.Helper()
	var mu sync.Mutex
	calls := &[]openCall{}
	oldLookup, oldOpen, oldCheck, oldQuery := lookupDockerEndpoints, openTunnel, checkTunnel, queryRemoteSocket
	t.Cleanup(func() {
		lookupDockerEndpoints, openTunnel, checkTunnel, queryRemoteSocket = oldLookup, oldOpen, oldCheck, oldQuery
	})

	lookupDockerEndpoints = func(context.Context, []string) map[string]string { return endpoints }
	openTunnel = func(_ context.Context, _ *sshtunnel.Manager, ep sshtunnel.Endpoint, local, remote string) (tunnelHandle, error) {
		mu.Lock()
		*calls = append(*calls, openCall{ep.Dest, remote})
		mu.Unlock()
		if err := openErr[ep.Dest]; err != nil {
			return nil, err
		}
		_ = os.Remove(local)
		ln, err := net.Listen("unix", local)
		if err != nil {
			return nil, err
		}
		se := ""
		if stderrFor != nil {
			se = stderrFor(remote)
		}
		return &fakeTunnel{ln: ln, stderr: se}, nil
	}
	checkTunnel = check
	queryRemoteSocket = func(context.Context, sshtunnel.Endpoint) (string, error) { return remoteSocket, nil }
	return calls
}

func newTunnelCmd(t *testing.T) (root, leaf *cobra.Command) {
	root = &cobra.Command{Use: "dockform"}
	leaf = &cobra.Command{Use: "plan"}
	root.AddCommand(leaf)
	root.SetContext(context.Background())
	return root, leaf
}

func okCheck(context.Context, string, string) error { return nil }

func TestTunnelTargets(t *testing.T) {
	cfg := &manifest.Config{Contexts: map[string]manifest.ContextConfig{
		"override": {Host: "ssh://u@h:2222"},
		"unixhost": {Host: "unix:///var/run/docker.sock"},
		"default":  {},
		"hetzner":  {},
		"local":    {},
		"ghost":    {},
	}}
	endpoints := map[string]string{"hetzner": "ssh://hetzner", "local": "unix:///var/run/docker.sock", "default": "ssh://nope"}
	got := tunnelTargets(cfg, endpoints)
	want := map[string]sshtunnel.Endpoint{
		"override": {Dest: "u@h", Port: "2222"},
		"hetzner":  {Dest: "hetzner"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tunnelTargets = %+v, want %+v", got, want)
	}
}

func TestActivateSSHTunnels_RewritesSSHHostsAndTearsDown(t *testing.T) {
	stubTunnelDeps(t, map[string]string{"remote": "ssh://remote"}, nil, nil, okCheck, "")
	root, leaf := newTunnelCmd(t)
	cfg := &manifest.Config{Contexts: map[string]manifest.ContextConfig{
		"remote": {},
		"local":  {Host: "unix:///var/run/docker.sock"},
	}}

	if err := ActivateSSHTunnels(leaf, cfg); err != nil {
		t.Fatalf("ActivateSSHTunnels: %v", err)
	}
	host := cfg.Contexts["remote"].Host
	if !strings.HasPrefix(host, "unix://") || !strings.HasSuffix(host, sshtunnel.SocketSuffix) {
		t.Fatalf("remote context should point at a tunnel socket, got %q", host)
	}
	if cfg.Contexts["local"].Host != "unix:///var/run/docker.sock" {
		t.Errorf("non-ssh context must be left alone, got %q", cfg.Contexts["local"].Host)
	}
	dir := filepath.Dir(strings.TrimPrefix(host, "unix://"))
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("tunnel dir should exist during the run: %v", err)
	}

	TeardownSSHTransport(root)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("tunnel dir should be removed after teardown, stat err = %v", err)
	}
}

func TestActivateSSHTunnels_OpenFailureNamesContextAndCleansUp(t *testing.T) {
	stubTunnelDeps(t, map[string]string{"remote": "ssh://remote"},
		map[string]error{"remote": errors.New("ssh exited before the tunnel was ready: Permission denied (publickey)")},
		nil, okCheck, "")
	root, leaf := newTunnelCmd(t)
	cfg := &manifest.Config{Contexts: map[string]manifest.ContextConfig{"remote": {}}}

	err := ActivateSSHTunnels(leaf, cfg)
	if err == nil {
		t.Fatal("expected an error when the tunnel cannot open")
	}
	if !strings.Contains(err.Error(), "remote") || !strings.Contains(err.Error(), "Permission denied") {
		t.Errorf("error should name the context and carry ssh's reason, got: %v", err)
	}
	if cfg.Contexts["remote"].Host != "" {
		t.Errorf("host must not be rewritten after a failure, got %q", cfg.Contexts["remote"].Host)
	}
	if v := root.Context().Value(sshTunnelKey{}); v != nil {
		t.Error("no tunnel manager should remain installed after a failure")
	}
}

// Rootless Docker listens somewhere other than /var/run/docker.sock. When the
// forward reaches the host but the socket is absent, look up the real path on
// the host and reopen once.
func TestActivateSSHTunnels_FallsBackToRemoteSocketPath(t *testing.T) {
	const rootless = "/run/user/1000/docker.sock"
	var n int
	var mu sync.Mutex
	calls := stubTunnelDeps(t, map[string]string{"remote": "ssh://remote"}, nil,
		func(remote string) string {
			if remote == sshtunnel.DefaultRemoteSocket {
				return "channel 1: open failed: connect failed: open failed\n"
			}
			return ""
		},
		func(context.Context, string, string) error {
			mu.Lock()
			defer mu.Unlock()
			n++
			if n == 1 {
				return errors.New("EOF")
			}
			return nil
		},
		rootless)
	root, leaf := newTunnelCmd(t)
	cfg := &manifest.Config{Contexts: map[string]manifest.ContextConfig{"remote": {}}}
	defer TeardownSSHTransport(root)

	if err := ActivateSSHTunnels(leaf, cfg); err != nil {
		t.Fatalf("ActivateSSHTunnels: %v", err)
	}
	if len(*calls) != 2 || (*calls)[0].remote != sshtunnel.DefaultRemoteSocket || (*calls)[1].remote != rootless {
		t.Errorf("expected default socket then %s, got %+v", rootless, *calls)
	}
}

func TestActivateSSHTunnels_NoSSHContextsInstallsNothing(t *testing.T) {
	stubTunnelDeps(t, map[string]string{"local": "unix:///var/run/docker.sock"}, nil, nil, okCheck, "")
	root, leaf := newTunnelCmd(t)
	cfg := &manifest.Config{Contexts: map[string]manifest.ContextConfig{"local": {}}}

	if err := ActivateSSHTunnels(leaf, cfg); err != nil {
		t.Fatalf("ActivateSSHTunnels: %v", err)
	}
	if v := root.Context().Value(sshTunnelKey{}); v != nil {
		t.Error("no manager should be installed when there is nothing to tunnel")
	}
}
