package common

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/cli/clitest"
	"github.com/gcstr/dockform/internal/manifest"
)

const reachabilityStub = `#!/bin/sh
case "$1" in
  version)
    case "$DOCKER_CONTEXT" in
      down|down2) echo 'cannot connect to daemon' >&2; exit 1 ;;
      *) echo '27.0.0'; exit 0 ;;
    esac ;;
esac
exit 0
`

const reachabilityTimeoutStub = `#!/bin/sh
case "$1" in
  version)
    case "$DOCKER_CONTEXT" in
      slow) exec sleep 2 ;;
      *) echo '27.0.0'; exit 0 ;;
    esac ;;
esac
exit 0
`

func TestEnsureContextsReachable(t *testing.T) {
	t.Run("all reachable", func(t *testing.T) {
		restore := clitest.WithCustomDockerStub(t, reachabilityStub)
		defer restore()

		factory := CreateClientFactory()
		cfg := &manifest.Config{
			Identifier: "demo",
			Contexts: map[string]manifest.ContextConfig{
				"a": {},
				"b": {},
			},
		}
		if err := EnsureContextsReachable(context.Background(), cfg, factory); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("single unreachable", func(t *testing.T) {
		restore := clitest.WithCustomDockerStub(t, reachabilityStub)
		defer restore()

		factory := CreateClientFactory()
		cfg := &manifest.Config{
			Identifier: "demo",
			Contexts: map[string]manifest.ContextConfig{
				"a":    {},
				"down": {},
			},
		}
		err := EnsureContextsReachable(context.Background(), cfg, factory)
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if !apperr.IsKind(err, apperr.Unavailable) {
			t.Errorf("expected Unavailable kind, got %v", err)
		}
		msg := err.Error()
		if !strings.Contains(msg, "down") {
			t.Errorf("expected error to contain 'down', got: %s", msg)
		}
		if !strings.Contains(msg, "1 context is unreachable") {
			t.Errorf("expected '1 context is unreachable' in error, got: %s", msg)
		}
	})

	t.Run("multiple unreachable sorted", func(t *testing.T) {
		restore := clitest.WithCustomDockerStub(t, reachabilityStub)
		defer restore()

		factory := CreateClientFactory()
		cfg := &manifest.Config{
			Identifier: "demo",
			Contexts: map[string]manifest.ContextConfig{
				"down":  {},
				"down2": {},
				"ok":    {},
			},
		}
		err := EnsureContextsReachable(context.Background(), cfg, factory)
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "down") {
			t.Errorf("expected error to contain 'down', got: %s", msg)
		}
		if !strings.Contains(msg, "down2") {
			t.Errorf("expected error to contain 'down2', got: %s", msg)
		}
		if !strings.Contains(msg, "contexts are unreachable") {
			t.Errorf("expected 'contexts are unreachable' in error, got: %s", msg)
		}
		// Verify sorted order: down appears before down2
		iDown := strings.Index(msg, "• down:")
		iDown2 := strings.Index(msg, "• down2:")
		if iDown < 0 {
			t.Fatalf("could not locate '• down:' in error message: %s", msg)
		}
		if iDown2 < 0 {
			t.Fatalf("could not locate '• down2:' in error message: %s", msg)
		}
		if iDown > iDown2 {
			t.Errorf("expected 'down' before 'down2' in error message, got: %s", msg)
		}
	})

	t.Run("zero contexts", func(t *testing.T) {
		restore := clitest.WithCustomDockerStub(t, reachabilityStub)
		defer restore()

		factory := CreateClientFactory()
		cfg := &manifest.Config{
			Identifier: "demo",
			Contexts:   map[string]manifest.ContextConfig{},
		}
		if err := EnsureContextsReachable(context.Background(), cfg, factory); err != nil {
			t.Fatalf("expected nil for empty contexts, got %v", err)
		}
	})
}

func TestEnsureContextsReachable_DownContextsProbedOnce(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "calls.txt")
	t.Setenv("DF_PROBE_COUNTER", counter)

	// Stub: every reachable context returns a version; every `down*` context
	// records one invocation and fails with an SSH-connection error (the only
	// kind the retry path would retry). If retries were active, the count would
	// be 5 per down context instead of 1.
	stub := `#!/bin/sh
case "$1" in
  version)
    case "$DOCKER_CONTEXT" in
      down|down2|down3)
        echo x >> "$DF_PROBE_COUNTER"
        echo 'kex_exchange_identification: Connection reset by peer' >&2; exit 1 ;;
      *) echo '27.0.0'; exit 0 ;;
    esac ;;
esac
exit 0
`
	restore := clitest.WithCustomDockerStub(t, stub)
	defer restore()

	factory := CreateClientFactory()
	cfg := &manifest.Config{
		Identifier: "demo",
		Contexts: map[string]manifest.ContextConfig{
			"down": {}, "down2": {}, "down3": {}, "ok": {},
		},
	}
	if err := EnsureContextsReachable(context.Background(), cfg, factory); err == nil {
		t.Fatal("expected unreachable error")
	}

	b, _ := os.ReadFile(counter)
	if got := strings.Count(string(b), "x\n"); got != 3 {
		t.Fatalf("expected exactly 3 probe invocations (one per down context), got %d", got)
	}
}

func TestEnsureContextsReachable_Timeout(t *testing.T) {
	restore := clitest.WithCustomDockerStub(t, reachabilityTimeoutStub)
	defer restore()

	// Shorten the timeout so the slow context is bounded well under its 2s sleep.
	old := ReachabilityProbeTimeout
	ReachabilityProbeTimeout = 200 * time.Millisecond
	defer func() { ReachabilityProbeTimeout = old }()

	factory := CreateClientFactory()
	cfg := &manifest.Config{
		Identifier: "demo",
		Contexts: map[string]manifest.ContextConfig{
			"ok":   {},
			"slow": {},
		},
	}

	start := time.Now()
	err := EnsureContextsReachable(context.Background(), cfg, factory)
	elapsed := time.Since(start)

	// The probe must have been bounded — well under the 2s sleep.
	if elapsed >= 1500*time.Millisecond {
		t.Errorf("EnsureContextsReachable took %s; expected < 1500ms (timeout not applied)", elapsed)
	}

	if err == nil {
		t.Fatal("expected non-nil error for unreachable slow context")
	}
	if !apperr.IsKind(err, apperr.Unavailable) {
		t.Errorf("expected Unavailable kind, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "slow") {
		t.Errorf("expected error to mention 'slow', got: %s", msg)
	}
	if !strings.Contains(msg, "timed out") {
		t.Errorf("expected error to contain 'timed out', got: %s", msg)
	}
}
