// Package sshtunnel carries the Docker Engine API for one host over a single
// forwarded SSH connection:
//
//	ssh -L <local.sock>:<remote docker.sock> <dest> 'cat >/dev/null'
//
// Unlike docker's ssh:// connection helper, which opens an SSH session per API
// connection, a forward multiplexes every connection as a forwarding channel.
// sshd's MaxSessions does not apply to those, so a stack of any size is served
// by one connection. The remote `cat` exits when its stdin closes, which ties
// the tunnel's lifetime to dockform's even if dockform is killed outright.
package sshtunnel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SocketSuffix ends every local tunnel socket. dockercli uses it to recognise a
// tunnel-backed host so the per-host concurrency cap still applies.
const SocketSuffix = ".tunnel.sock"

// DefaultRemoteSocket is where rootful Docker listens on the remote host.
const DefaultRemoteSocket = "/var/run/docker.sock"

// Endpoint is an ssh:// Docker endpoint reduced to what ssh needs.
type Endpoint struct {
	Dest string // [user@]host, handed to ssh unchanged so ~/.ssh/config applies
	Port string
}

// ParseEndpoint parses an ssh://[user@]host[:port] Docker endpoint.
func ParseEndpoint(raw string) (Endpoint, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "ssh" || u.Hostname() == "" {
		return Endpoint{}, fmt.Errorf("not an ssh:// endpoint: %q", raw)
	}
	dest := u.Hostname()
	if u.User != nil && u.User.Username() != "" {
		dest = u.User.Username() + "@" + dest
	}
	return Endpoint{Dest: dest, Port: u.Port()}, nil
}

// Args builds the ssh argv (without the binary) for a tunnel.
//
// ControlPath=none keeps the tunnel off any ControlMaster the user configured.
// ExitOnForwardFailure makes a failed bind fatal instead of leaving a live ssh
// with no forward, and StreamLocalBindUnlink removes a stale socket file a
// previous ssh may have left behind.
func Args(ep Endpoint, localSock, remoteSock string) []string {
	args := []string{
		"-T",
		"-o", "ControlPath=none",
		"-o", "ControlMaster=no",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "StreamLocalBindUnlink=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ConnectTimeout=30",
		// Forward failures are logged at INFO. Command-line -o beats the user's
		// config, so a LogLevel ERROR/QUIET there cannot hide them.
		"-o", "LogLevel=INFO",
	}
	if ep.Port != "" {
		args = append(args, "-p", ep.Port)
	}
	return append(args, "-L", localSock+":"+remoteSock, "--", ep.Dest, "cat >/dev/null")
}

// IsRemoteSocketMissing reports whether ssh's stderr shows the forward reaching
// the host but failing to connect to the remote Docker socket.
func IsRemoteSocketMissing(stderr string) bool {
	return strings.Contains(stderr, "open failed: connect failed")
}

// Tunnel is one running ssh -L process.
type Tunnel struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *syncBuffer
	done   chan struct{}
}

// Done is closed when the ssh process exits.
func (t *Tunnel) Done() <-chan struct{} { return t.done }

// Stderr returns everything ssh has written to stderr so far.
func (t *Tunnel) Stderr() string { return t.stderr.String() }

// Close ends the tunnel. Idempotent.
func (t *Tunnel) Close() { t.close() }

// close ends the tunnel: closing stdin lets the remote `cat` exit and ssh
// follow; if it has not gone within the grace period it is killed.
func (t *Tunnel) close() {
	_ = t.stdin.Close()
	select {
	case <-t.done:
		return
	case <-time.After(3 * time.Second):
	}
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	<-t.done
}

// Manager owns a run's tunnels and their private socket directory.
type Manager struct {
	dir     string
	mu      sync.Mutex
	tunnels []*Tunnel
	closed  bool
}

// NewManager creates the run's socket directory. It lives under /tmp, not
// $TMPDIR, for the same reason as sshmux: macOS's $TMPDIR is long enough to
// push socket paths past the ~104-character unix socket limit. MkdirTemp
// creates it 0700, so no other local user can reach the sockets inside.
func NewManager() (*Manager, error) {
	dir, err := os.MkdirTemp("/tmp", "dftun-")
	if err != nil {
		return nil, err
	}
	return &Manager{dir: dir}, nil
}

// Dir returns the private socket directory.
func (m *Manager) Dir() string { return m.dir }

// SocketPath returns the local socket path for the i-th tunnel.
func (m *Manager) SocketPath(i int) string {
	return filepath.Join(m.dir, strconv.Itoa(i)+SocketSuffix)
}

// Open starts an ssh -L tunnel and returns once localSock accepts connections.
// It fails if ssh exits first (the error carries ssh's stderr), if ready
// elapses, or if ctx is cancelled. The tunnel is closed by Manager.Close.
func (m *Manager) Open(ctx context.Context, sshPath string, ep Endpoint, localSock, remoteSock string, ready time.Duration) (*Tunnel, error) {
	cmd := exec.Command(sshPath, Args(ep, localSock, remoteSock)...)
	// Own process group: a terminal Ctrl-C must not kill the tunnel under
	// in-flight work. Teardown ends it, and a killed dockform ends it via stdin.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	t := &Tunnel{cmd: cmd, stdin: stdin, stderr: &syncBuffer{}, done: make(chan struct{})}
	cmd.Stderr = t.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ssh: %w", err)
	}
	go func() {
		_ = cmd.Wait()
		close(t.done)
	}()

	m.mu.Lock()
	m.tunnels = append(m.tunnels, t)
	m.mu.Unlock()

	deadline := time.NewTimer(ready)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-t.done:
			return nil, fmt.Errorf("ssh exited before the tunnel was ready: %s", strings.TrimSpace(t.Stderr()))
		case <-ctx.Done():
			t.close()
			return nil, ctx.Err()
		case <-deadline.C:
			t.close()
			return nil, fmt.Errorf("tunnel not ready after %s: %s", ready, strings.TrimSpace(t.Stderr()))
		case <-tick.C:
			if c, err := net.DialTimeout("unix", localSock, 200*time.Millisecond); err == nil {
				_ = c.Close()
				return t, nil
			}
		}
	}
}

// Close ends every tunnel and removes the socket directory. Idempotent and
// nil-safe.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	tunnels := m.tunnels
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, t := range tunnels {
		wg.Add(1)
		go func(t *Tunnel) {
			defer wg.Done()
			t.close()
		}(t)
	}
	wg.Wait()
	_ = os.RemoveAll(m.dir)
}

// syncBuffer is a bytes.Buffer safe for exec's stderr copier and readers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Reason turns ssh's stderr into a short plain-language cause, or "" when it
// does not recognise the output. Patterns come from real OpenSSH output.
func Reason(stderr string) string {
	switch {
	case strings.Contains(stderr, "Permission denied"):
		return "SSH authentication failed (check your key or ssh-agent)"
	case strings.Contains(stderr, "Could not resolve hostname"):
		return "the host name could not be resolved"
	case strings.Contains(stderr, "Host key verification failed"):
		return "the host key could not be verified (check known_hosts)"
	case strings.Contains(stderr, "Connection refused"):
		return "nothing is accepting SSH connections on that host and port"
	case strings.Contains(stderr, "Operation timed out"),
		strings.Contains(stderr, "Connection timed out"),
		strings.Contains(stderr, "No route to host"),
		strings.Contains(stderr, "Network is unreachable"):
		return "the host is unreachable"
	}
	return ""
}
