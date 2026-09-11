package sshtunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as a fake `ssh` so the tunnel lifecycle can be tested without
// a real host. When GO_WANT_FAKE_SSH=1 the test binary behaves like ssh -L:
// it listens on the local socket named by -L and blocks until stdin closes.
func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_FAKE_SSH") == "1" {
		os.Exit(fakeSSH(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeSSH(args []string) int {
	if os.Getenv("FAKE_SSH_MODE") == "fail" {
		fmt.Fprintln(os.Stderr, "ssh: connect to host h port 22: Connection refused")
		return 255
	}
	var local string
	for i, a := range args {
		if a == "-L" && i+1 < len(args) {
			local = strings.SplitN(args[i+1], ":", 2)[0]
		}
	}
	ln, err := net.Listen("unix", local)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake ssh listen:", err)
		return 255
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	// Exit when stdin closes, exactly like the remote `cat >/dev/null` does.
	_, _ = io.Copy(io.Discard, bufio.NewReader(os.Stdin))
	return 0
}

func TestParseEndpoint(t *testing.T) {
	cases := []struct {
		in       string
		wantDest string
		wantPort string
		wantErr  bool
	}{
		{in: "ssh://hetzner-two", wantDest: "hetzner-two"},
		{in: "ssh://gcstr@digitalocean", wantDest: "gcstr@digitalocean"},
		{in: "ssh://u@h:2222", wantDest: "u@h", wantPort: "2222"},
		{in: "unix:///var/run/docker.sock", wantErr: true},
		{in: "ssh://", wantErr: true},
	}
	for _, tc := range cases {
		ep, err := ParseEndpoint(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: expected error, got %+v", tc.in, ep)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.in, err)
			continue
		}
		if ep.Dest != tc.wantDest || ep.Port != tc.wantPort {
			t.Errorf("%q: got dest=%q port=%q, want dest=%q port=%q", tc.in, ep.Dest, ep.Port, tc.wantDest, tc.wantPort)
		}
	}
}

func TestArgs(t *testing.T) {
	args := Args(Endpoint{Dest: "u@h", Port: "2222"}, "/tmp/d/0.tunnel.sock", "/var/run/docker.sock")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"ControlPath=none", "ControlMaster=no", "ExitOnForwardFailure=yes",
		"StreamLocalBindUnlink=yes", "ServerAliveInterval=15", "ServerAliveCountMax=3",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
	if !slices.Contains(args, "-T") {
		t.Errorf("args must disable the pty (-T): %v", args)
	}
	if i := slices.Index(args, "-L"); i < 0 || args[i+1] != "/tmp/d/0.tunnel.sock:/var/run/docker.sock" {
		t.Errorf("args must forward local socket to remote socket: %v", args)
	}
	if i := slices.Index(args, "-p"); i < 0 || args[i+1] != "2222" {
		t.Errorf("args must carry the port: %v", args)
	}
	// Destination then the remote command that exits when dockform goes away.
	if n := len(args); n < 2 || args[n-2] != "u@h" || args[n-1] != "cat >/dev/null" {
		t.Errorf("args must end with destination and 'cat >/dev/null': %v", args)
	}
}

func TestArgs_NoPort(t *testing.T) {
	if slices.Contains(Args(Endpoint{Dest: "h"}, "/a", "/b"), "-p") {
		t.Error("no -p when the endpoint has no port")
	}
}

func TestIsRemoteSocketMissing(t *testing.T) {
	if !IsRemoteSocketMissing("channel 1: open failed: connect failed: open failed\n") {
		t.Error("should recognise ssh's channel open failure")
	}
	if IsRemoteSocketMissing("Connection closed by 10.0.0.1 port 22") {
		t.Error("an ordinary disconnect is not a missing socket")
	}
}

func TestManager_DirIsPrivate(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	fi, err := os.Stat(m.Dir())
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Errorf("tunnel dir mode = %o, want 700", perm)
	}
	if !strings.HasSuffix(m.SocketPath(3), "3"+SocketSuffix) {
		t.Errorf("socket path %q should end with 3%s", m.SocketPath(3), SocketSuffix)
	}
}

func TestOpen_ReadyThenCloseCleansUp(t *testing.T) {
	t.Setenv("GO_WANT_FAKE_SSH", "1")
	t.Setenv("FAKE_SSH_MODE", "ok")
	m, err := NewManager()
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	sock := m.SocketPath(0)
	tun, err := m.Open(context.Background(), os.Args[0], Endpoint{Dest: "h"}, sock, "/var/run/docker.sock", 5*time.Second)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("socket should accept connections once Open returns: %v", err)
	}
	_ = c.Close()

	dir := m.Dir()
	m.Close()

	select {
	case <-tun.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("ssh process still running after Close")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("tunnel dir should be removed after Close, stat err = %v", err)
	}
}

func TestOpen_FailureReturnsSSHStderr(t *testing.T) {
	t.Setenv("GO_WANT_FAKE_SSH", "1")
	t.Setenv("FAKE_SSH_MODE", "fail")
	m, err := NewManager()
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	_, err = m.Open(context.Background(), os.Args[0], Endpoint{Dest: "h"}, m.SocketPath(0), "/var/run/docker.sock", 5*time.Second)
	if err == nil {
		t.Fatal("expected Open to fail when ssh exits before the socket appears")
	}
	if !strings.Contains(err.Error(), "Connection refused") {
		t.Errorf("error should carry ssh's stderr, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.Dir(), "0"+SocketSuffix)); !os.IsNotExist(statErr) {
		t.Errorf("no socket should be left after a failed open")
	}
}

// ssh logs forward failures ("channel N: open failed: ...") at INFO. A user
// config with LogLevel ERROR or QUIET hides them, which would silently disable
// the rootless fallback and the error classification. Command-line -o beats
// the config file, so the tunnel forces INFO.
func TestArgs_ForcesInfoLogLevel(t *testing.T) {
	if !strings.Contains(strings.Join(Args(Endpoint{Dest: "h"}, "/a", "/b"), " "), "LogLevel=INFO") {
		t.Error("tunnel ssh must force LogLevel=INFO")
	}
}

// The stderr lines below were captured from real ssh (OpenSSH 10.3) runs.
func TestReason(t *testing.T) {
	cases := []struct{ stderr, want string }{
		{"gustavocastro@127.0.0.1: Permission denied (publickey).", "SSH authentication failed"},
		{"ssh: Could not resolve hostname dockform-no-such-host.invalid: nodename nor servname provided, or not known", "host name could not be resolved"},
		{"ssh: connect to host 127.0.0.1 port 2293: Connection refused", "nothing is accepting SSH connections"},
		{"ssh: connect to host 10.255.255.1 port 22: Operation timed out", "host is unreachable"},
		{"ssh: connect to host h port 22: No route to host", "host is unreachable"},
		{"Host key verification failed.", "host key could not be verified"},
		{"something ssh has never said", ""},
	}
	for _, tc := range cases {
		got := Reason(tc.stderr)
		if tc.want == "" {
			if got != "" {
				t.Errorf("Reason(%q) = %q, want no classification", tc.stderr, got)
			}
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("Reason(%q) = %q, want it to mention %q", tc.stderr, got, tc.want)
		}
	}
}
