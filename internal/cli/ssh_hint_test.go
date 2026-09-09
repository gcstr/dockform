package cli

import (
	"strings"
	"testing"
)

// The definite MaxSessions signature. sshd refused a channel because the
// per-connection session limit is exhausted.
func TestComposeStderrHint_SSHSessionExhaustion(t *testing.T) {
	stderr := "error during connect: Get \"http://docker.example.com/v1.54/containers/abc/json\": " +
		"command [ssh -o ConnectTimeout=30 -T -- hetzner-two docker system dial-stdio] has exited with exit status 255: " +
		"stderr=mux_client_request_session: session request failed: Session open refused by peer\n" +
		"Connection closed by 10.0.0.1 port 22"

	hint := composeStderrHint(stderr)
	if hint == "" {
		t.Fatal("expected a hint for SSH session exhaustion, got none")
	}
	for _, want := range []string{"MaxSessions", "sshd_config", "split"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint should mention %q; got:\n%s", want, hint)
		}
	}
	// The single most important line: stop the reader reaching for the
	// parallelism knob, which is measurably useless for this failure.
	if !strings.Contains(hint, "--parallel") {
		t.Errorf("hint must say --parallel will not help; got:\n%s", hint)
	}
	if strings.Contains(strings.ToLower(hint), "compose file") {
		t.Errorf("must not blame compose files; got:\n%s", hint)
	}
}

// The ambiguous signature seen when multiplexing is off: a bare connection
// close, which is indistinguishable from a host that is genuinely down. The
// hint must raise the possibility without asserting it.
func TestComposeStderrHint_SSHConnectionRefused_IsHedged(t *testing.T) {
	stderr := "error during connect: Get \"http://docker.example.com/v1.54/images/alpine/json\": " +
		"command [ssh -T -- hetzner-two docker system dial-stdio] has exited with exit status 255: " +
		"stderr=Connection closed by 10.0.0.1 port 22"

	hint := composeStderrHint(stderr)
	if hint == "" {
		t.Fatal("expected a hint for a refused SSH connection, got none")
	}
	if !strings.Contains(hint, "MaxStartups") {
		t.Errorf("hint should mention the connection-rate limit; got:\n%s", hint)
	}
	// Must not assert a cause it cannot know.
	low := strings.ToLower(hint)
	if !strings.Contains(low, "may") && !strings.Contains(low, "might") && !strings.Contains(low, "either") {
		t.Errorf("hint must be hedged, not assertive; got:\n%s", hint)
	}
}

// Registry/image/disk hints must keep winning over the SSH ones.
func TestComposeStderrHint_KnownPatternsStillWin(t *testing.T) {
	if h := composeStderrHint("Error response from daemon: pull access denied for foo/bar"); !strings.Contains(h, "Registry authentication") {
		t.Errorf("auth hint regressed: %q", h)
	}
	if h := composeStderrHint("no space left on device"); !strings.Contains(h, "disk space") {
		t.Errorf("disk hint regressed: %q", h)
	}
}
