package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
)

// captureHintStderr runs provideExternalErrorHints with stderr redirected and
// returns whatever it printed.
func captureHintStderr(t *testing.T, err error) string {
	t.Helper()
	old := os.Stderr
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("pipe: %v", pipeErr)
	}
	os.Stderr = w
	provideExternalErrorHints(err)
	_ = w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)
	return string(out)
}

// The operation name for any compose-phase failure contains the word "compose"
// (e.g. "compose up hetzner-one/navidrome"). That must not, on its own, produce
// a hint blaming the compose files and the daemon — the real cause here is a
// missing external volume, and both the compose file and the daemon are fine.
func TestHints_UnrelatedFailureInComposeOperation_NoComposeFileHint(t *testing.T) {
	inner := apperr.New("dockercli.Exec", apperr.External,
		"service \"navidrome\" refers to undefined volume navidrome_data")
	err := apperr.Wrap("planner.Apply", apperr.External, inner, "compose up hetzner-one/navidrome")

	out := captureHintStderr(t, err)

	if strings.Contains(out, "Check your compose files") {
		t.Errorf("must not blame compose files for an unrelated failure; got:\n%s", out)
	}
	if strings.Contains(out, "Docker Compose operation failed") {
		t.Errorf("must not assert a compose-operation cause from the operation name; got:\n%s", out)
	}
}

// An SSH transport failure inside a compose operation must likewise not be
// attributed to the compose files.
func TestHints_SSHFailureInComposeOperation_NoComposeFileHint(t *testing.T) {
	inner := apperr.New("dockercli.Exec", apperr.External,
		"mux_client_request_session: session request failed: Session open refused by peer\nConnection closed by 10.0.0.1 port 22")
	err := apperr.Wrap("planner.Apply", apperr.External, inner, "compose up hetzner-two/dawarich")

	out := captureHintStderr(t, err)

	if strings.Contains(out, "Check your compose files") {
		t.Errorf("must not blame compose files for an SSH failure; got:\n%s", out)
	}
}

// Regression guard: hints driven by actual captured stderr must keep working.
func TestHints_KnownStderrPatternStillFires(t *testing.T) {
	inner := apperr.New("dockercli.Exec", apperr.External,
		"Error response from daemon: pull access denied for foo/bar")
	err := apperr.Wrap("planner.Apply", apperr.External, inner, "compose up ctx/stack")

	out := captureHintStderr(t, err)

	if !strings.Contains(out, "Registry authentication problem") {
		t.Errorf("evidence-based hint should still fire; got:\n%s", out)
	}
}

// The multi-context path emits its generic hint as an unconditional else, so a
// failure that has nothing to do with compose (here: a service restart) is told
// to check its compose files. This is the path a multi-context apply takes.
func TestHints_MultiError_NonComposeFailure_NoComposeFileHint(t *testing.T) {
	restartFail := apperr.Wrap("restartmanager.RestartPendingServices", apperr.External,
		apperr.New("dockercli.Exec", apperr.External, "container traefik is unhealthy"),
		"restart service traefik")

	multi := &apperr.MultiError{Errors: []error{restartFail}}

	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	printMultiErrorDetail(multi)
	_ = w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)
	s := string(out)

	if !strings.Contains(s, "container traefik is unhealthy") {
		t.Errorf("child detail should still be printed; got:\n%s", s)
	}
	if strings.Contains(s, "Check your compose files") {
		t.Errorf("a restart failure must not be blamed on compose files; got:\n%s", s)
	}
}
