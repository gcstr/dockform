package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/cli/common"
)

func captureUserFriendly(t *testing.T, err error) string {
	t.Helper()
	old := os.Stderr
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("pipe: %v", pipeErr)
	}
	os.Stderr = w
	printUserFriendly(err)
	_ = w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)
	return string(out)
}

// A tunnel that cannot open is an SSH problem, not a Docker daemon problem. It
// must not get the "Is the Docker daemon running?" hint, and must point at the
// escape hatch instead.
func TestPrintUserFriendly_TunnelFailureSkipsDockerHint(t *testing.T) {
	err := apperr.Wrap("common.ActivateSSHTunnels", apperr.Unavailable, common.ErrSSHTunnel,
		"could not open an SSH tunnel:\n  • hetzner-two (hetzner-two): the host is unreachable")
	out := captureUserFriendly(t, err)

	if strings.Contains(out, "Is the Docker daemon running") {
		t.Errorf("tunnel failure must not get the docker daemon hint; got:\n%s", out)
	}
	if !strings.Contains(out, "--ssh-transport=mux") {
		t.Errorf("tunnel failure should point at --ssh-transport=mux; got:\n%s", out)
	}
}

// A plain unreachable daemon keeps its existing hint.
func TestPrintUserFriendly_UnreachableDaemonKeepsDockerHint(t *testing.T) {
	err := apperr.New("dockercli.CheckDaemon", apperr.Unavailable, "docker daemon not reachable (context=hetzner-two)")
	if out := captureUserFriendly(t, err); !strings.Contains(out, "Is the Docker daemon running") {
		t.Errorf("unreachable daemon should keep the docker hint; got:\n%s", out)
	}
}

// Observed when the tunnel dies mid-apply: in-flight requests end with EOF,
// and new connections hit a socket file with no listener.
func TestComposeStderrHint_TunnelDiedMidRun(t *testing.T) {
	for _, stderr := range []string{
		`error during connect: Post "http://%2Ftmp%2Fdftun-12%2F0.tunnel.sock/v1.54/containers/48b8/start": EOF`,
		`Cannot connect to the Docker daemon at unix:///tmp/dftun-12/0.tunnel.sock. Is the docker daemon running?`,
	} {
		hint := composeStderrHint(stderr)
		if !strings.Contains(hint, "SSH tunnel") || !strings.Contains(strings.ToLower(hint), "re-run") {
			t.Errorf("expected a tunnel-closed hint that says to re-run, got %q for:\n%s", hint, stderr)
		}
	}
}

// The same daemon error on the local socket is not a tunnel problem.
func TestComposeStderrHint_LocalSocketIsNotATunnel(t *testing.T) {
	hint := composeStderrHint("Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?")
	if strings.Contains(hint, "SSH tunnel") {
		t.Errorf("local socket failure must not get the tunnel hint, got %q", hint)
	}
}
