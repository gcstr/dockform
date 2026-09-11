package common

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/sshtunnel"
	"github.com/spf13/cobra"
)

// AnnotationSSHTunnel set to "off" keeps a command off the tunnel transport.
// The dashboard uses it: it runs for as long as it is open, so it stays on the
// multiplexer, which re-dials on its own if the connection drops.
const AnnotationSSHTunnel = "dockform.ssh-tunnel"

// tunnelReadyTimeout bounds how long ssh gets to authenticate and bind.
var tunnelReadyTimeout = 30 * time.Second

type sshTunnelKey struct{}

// tunnelHandle is the part of *sshtunnel.Tunnel activation needs; tests fake it.
type tunnelHandle interface {
	Stderr() string
	Close()
}

// Hooks that touch ssh and docker, replaced in tests.
var (
	lookupDockerEndpoints = dockerContextEndpoints
	openTunnel            = func(ctx context.Context, m *sshtunnel.Manager, ep sshtunnel.Endpoint, local, remote string) (tunnelHandle, error) {
		sshPath, err := exec.LookPath("ssh")
		if err != nil {
			return nil, fmt.Errorf("ssh not found on PATH: %w", err)
		}
		t, err := m.Open(ctx, sshPath, ep, local, remote, tunnelReadyTimeout)
		if err != nil {
			return nil, err
		}
		return t, nil
	}
	checkTunnel = func(ctx context.Context, name, local string) error {
		probeCtx, cancel := context.WithTimeout(ctx, ReachabilityProbeTimeout)
		defer cancel()
		return dockercli.NewWithHost(name, "unix://"+local).CheckDaemon(probeCtx)
	}
	queryRemoteSocket = remoteDockerSocket
)

// EffectiveSSHTransport applies per-command restrictions to the resolved
// transport: a command annotated AnnotationSSHTunnel=off never tunnels.
func EffectiveSSHTransport(cmd *cobra.Command, t SSHTransport) SSHTransport {
	if t == SSHTransportTunnel && cmd != nil && cmd.Annotations[AnnotationSSHTunnel] == "off" {
		return SSHTransportMux
	}
	return t
}

// ActivateSSHTunnels opens one tunnel per ssh:// context and points that
// context at the tunnel socket, so every docker client built afterwards goes
// through it. It must run before any client for those contexts exists. On any
// failure it opens nothing and returns an error naming each failed context.
func ActivateSSHTunnels(cmd *cobra.Command, cfg *manifest.Config) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	var lookup []string
	for name, cc := range cfg.Contexts {
		if cc.Host == "" && name != "" && name != "default" {
			lookup = append(lookup, name)
		}
	}
	targets := tunnelTargets(cfg, lookupDockerEndpoints(ctx, lookup))
	if len(targets) == 0 {
		return nil
	}

	m, err := sshtunnel.NewManager()
	if err != nil {
		return apperr.Wrap("common.ActivateSSHTunnels", apperr.Internal, err, "create tunnel directory")
	}
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		sockets = make(map[string]string, len(names))
		failed  []string
	)
	for i, name := range names {
		wg.Add(1)
		go func(name, local string, ep sshtunnel.Endpoint) {
			defer wg.Done()
			err := openChecked(ctx, m, name, ep, local)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, fmt.Sprintf("  • %s (%s): %v", name, ep.Dest, err))
				return
			}
			sockets[name] = local
		}(name, m.SocketPath(i), targets[name])
	}
	wg.Wait()

	if len(failed) > 0 {
		m.Close()
		sort.Strings(failed)
		return apperr.New("common.ActivateSSHTunnels", apperr.Unavailable,
			"could not open an SSH tunnel:\n%s\nTo connect without a tunnel, use --ssh-transport=mux.", strings.Join(failed, "\n"))
	}
	for name, local := range sockets {
		cc := cfg.Contexts[name]
		cc.Host = "unix://" + local
		cfg.Contexts[name] = cc
	}
	root := cmd.Root()
	base := root.Context()
	if base == nil {
		base = context.Background()
	}
	root.SetContext(context.WithValue(base, sshTunnelKey{}, m))
	return nil
}

// openChecked opens a tunnel to the default Docker socket and verifies the
// daemon answers through it. If the forward reaches the host but nothing
// listens there (rootless Docker), it asks the host for its socket path and
// reopens once.
func openChecked(ctx context.Context, m *sshtunnel.Manager, name string, ep sshtunnel.Endpoint, local string) error {
	tun, err := openTunnel(ctx, m, ep, local, sshtunnel.DefaultRemoteSocket)
	if err != nil {
		return err
	}
	checkErr := checkTunnel(ctx, name, local)
	if checkErr == nil {
		return nil
	}
	if !awaitRemoteSocketMissing(tun) {
		return checkErr
	}
	tun.Close()
	remote, err := queryRemoteSocket(ctx, ep)
	if err != nil {
		return fmt.Errorf("docker is not listening at %s on the host, and its socket path could not be looked up: %w", sshtunnel.DefaultRemoteSocket, err)
	}
	if remote == sshtunnel.DefaultRemoteSocket {
		return checkErr
	}
	if _, err := openTunnel(ctx, m, ep, local, remote); err != nil {
		return err
	}
	return checkTunnel(ctx, name, local)
}

// awaitRemoteSocketMissing gives ssh a moment to report a refused forward,
// which it writes asynchronously after the connection is rejected.
func awaitRemoteSocketMissing(t tunnelHandle) bool {
	deadline := time.Now().Add(time.Second)
	for {
		if sshtunnel.IsRemoteSocketMissing(t.Stderr()) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// tunnelTargets returns the contexts whose Docker endpoint is ssh://, either
// from a host override or from the named docker context's endpoint.
func tunnelTargets(cfg *manifest.Config, endpoints map[string]string) map[string]sshtunnel.Endpoint {
	out := map[string]sshtunnel.Endpoint{}
	for name, cc := range cfg.Contexts {
		raw := cc.Host
		if raw == "" {
			if name == "" || name == "default" {
				continue
			}
			raw = endpoints[name]
		}
		if !strings.HasPrefix(raw, "ssh://") {
			continue
		}
		if ep, err := sshtunnel.ParseEndpoint(raw); err == nil {
			out[name] = ep
		}
	}
	return out
}

// dockerContextEndpoints reads each docker context's Docker endpoint. Names
// that cannot be inspected are left out; the reachability check reports them.
func dockerContextEndpoints(ctx context.Context, names []string) map[string]string {
	out := make(map[string]string, len(names))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			b, err := exec.CommandContext(ctx, "docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}", name).Output()
			if err != nil {
				return
			}
			mu.Lock()
			out[name] = strings.TrimSpace(string(b))
			mu.Unlock()
		}(name)
	}
	wg.Wait()
	return out
}

// remoteDockerSocket asks the host's own docker CLI where its daemon listens.
func remoteDockerSocket(ctx context.Context, ep sshtunnel.Endpoint) (string, error) {
	args := []string{"-T", "-o", "ControlPath=none", "-o", "ControlMaster=no", "-o", "ConnectTimeout=30"}
	if ep.Port != "" {
		args = append(args, "-p", ep.Port)
	}
	args = append(args, "--", ep.Dest, "docker context inspect --format '{{.Endpoints.docker.Host}}'")
	b, err := exec.CommandContext(ctx, "ssh", args...).Output()
	if err != nil {
		return "", err
	}
	host := strings.TrimSpace(string(b))
	if !strings.HasPrefix(host, "unix://") {
		return "", fmt.Errorf("remote docker endpoint is %q, not a unix socket", host)
	}
	return strings.TrimPrefix(host, "unix://"), nil
}

// TeardownSSHTransport ends whatever SSH transport the command set up: the
// multiplexer's run dir and any tunnels. Idempotent and nil-safe.
func TeardownSSHTransport(cmd *cobra.Command) {
	TeardownSSHMux(cmd)
	if cmd == nil {
		return
	}
	root := cmd.Root()
	if root.Context() == nil {
		return
	}
	if m, ok := root.Context().Value(sshTunnelKey{}).(*sshtunnel.Manager); ok {
		m.Close()
	}
}
