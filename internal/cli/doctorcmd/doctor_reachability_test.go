package doctorcmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/common"
)

// writeMultiContextManifest materialises a minimal manifest with the given
// context names (no stacks needed — doctor only cares about cfg.Contexts).
func writeMultiContextManifest(t *testing.T, contextNames ...string) string {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("identifier: demo\ncontexts:\n")
	for _, name := range contextNames {
		b.WriteString("  " + name + ": {}\n")
	}
	path := filepath.Join(dir, "dockform.yml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// withMultiContextDoctorStub installs a docker stub whose "version" (and other
// daemon-touching) subcommands succeed for every context except those listed in
// downContexts, and whose "slow" context sleeps well past the reachability
// timeout so the hang-vs-timeout behavior can be asserted without waiting
// out a real dead SSH connection.
func withMultiContextDoctorStub(t *testing.T, downContexts ...string) func() {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix shell script; skipping on Windows")
	}

	downCase := ""
	for _, c := range downContexts {
		downCase += "    " + c + ") echo 'cannot connect to daemon' >&2; exit 1 ;;\n"
	}

	script := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    case "$DOCKER_CONTEXT" in
` + downCase + `    slow) exec sleep 5 ;;
    *) echo '27.0.0'; exit 0 ;;
    esac
    ;;
  context)
    echo '"unix:///var/run/docker.sock"'
    exit 0
    ;;
  compose)
    echo "2.29.0"
    exit 0
    ;;
  image)
    exit 0
    ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "create" ]; then exit 0; fi
    if [ "$sub" = "rm" ]; then exit 0; fi
    ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "create" ]; then exit 0; fi
    if [ "$sub" = "rm" ]; then exit 0; fi
    ;;
esac
exit 0
`
	return withDoctorStub(t, script)
}

// TestDoctorCmd_UnreachableContext_TimesOutInsteadOfHanging is the regression
// test for the original hang: a context whose daemon never responds must be
// reported as unreachable/failed within a bounded time, not hang forever.
func TestDoctorCmd_UnreachableContext_TimesOutInsteadOfHanging(t *testing.T) {
	defer withMultiContextDoctorStub(t)()

	// Shorten the reachability timeout so the test doesn't need to wait out
	// the stub's full 5s sleep to prove the probe is bounded.
	old := common.ReachabilityProbeTimeout
	common.ReachabilityProbeTimeout = 300 * time.Millisecond
	defer func() { common.ReachabilityProbeTimeout = old }()

	manifestPath := writeMultiContextManifest(t, "ok", "slow")

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor", "--manifest", manifestPath})

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- root.Execute() }()

	select {
	case <-done:
		elapsed := time.Since(start)
		if elapsed >= 3*time.Second {
			t.Fatalf("doctor took %s; expected it to time out well before the stub's 5s sleep", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("doctor hung: did not return within 10s for an unreachable context")
	}

	output := out.String()
	if !strings.Contains(output, `context:slow`) {
		t.Errorf("expected a per-context check for 'slow', got: %q", output)
	}
	if !strings.Contains(output, "unreachable") {
		t.Errorf("expected 'slow' context to report unreachable, got: %q", output)
	}
	if !strings.Contains(output, "FAIL") {
		t.Errorf("expected FAIL in summary due to unreachable context, got: %q", output)
	}
}

// TestDoctorCmd_MultipleContexts_ProbedIndependently verifies doctor probes
// every context configured in the manifest, in parallel, and reports pass/fail
// per context rather than validating only the active docker context.
func TestDoctorCmd_MultipleContexts_ProbedIndependently(t *testing.T) {
	defer withMultiContextDoctorStub(t, "down")()

	manifestPath := writeMultiContextManifest(t, "up1", "up2", "down")

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor", "--manifest", manifestPath})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected doctor to fail due to unreachable 'down' context")
	}

	output := out.String()
	for _, ctxName := range []string{"up1", "up2", "down"} {
		if !strings.Contains(output, "context:"+ctxName) {
			t.Errorf("expected a per-context check line for %q, got: %q", ctxName, output)
		}
	}
	// Reachable contexts should be marked pass.
	if !strings.Contains(output, `[context:up1] Context "up1" reachable — ok`) {
		t.Errorf("expected up1 to pass, got: %q", output)
	}
	if !strings.Contains(output, `[context:up2] Context "up2" reachable — ok`) {
		t.Errorf("expected up2 to pass, got: %q", output)
	}
	// The down context should be marked failed/unreachable.
	if !strings.Contains(output, `[context:down] Context "down" reachable — unreachable`) {
		t.Errorf("expected down to fail as unreachable, got: %q", output)
	}
	if !strings.Contains(output, "exit code 1") {
		t.Errorf("expected exit code 1 due to failed check, got: %q", output)
	}
}

// TestDoctorCmd_ContextFlag_ScopesToSingleContext verifies --context still
// narrows doctor to just the one named context (bounded), even when a manifest
// with other contexts is present, matching the pre-existing single-context
// override semantics.
func TestDoctorCmd_ContextFlag_ScopesToSingleContext(t *testing.T) {
	defer withMultiContextDoctorStub(t, "down")()

	// Manifest has three contexts, one of which ("down") is unreachable, but
	// --context should scope the probe to only "up1" and ignore the others.
	manifestPath := writeMultiContextManifest(t, "up1", "up2", "down")

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor", "--manifest", manifestPath, "--context", "up1"})

	if err := root.Execute(); err != nil {
		t.Fatalf("doctor command failed: %v", err)
	}

	output := out.String()
	if strings.Contains(output, "context:up2") || strings.Contains(output, "context:down") {
		t.Errorf("expected --context to scope to a single context, but other contexts were probed: %q", output)
	}
	if !strings.Contains(output, `Context: up1`) {
		t.Errorf("expected header to show the overridden context, got: %q", output)
	}
	if !strings.Contains(output, `[context] Active context reachable — "up1"`) {
		t.Errorf("expected single active-context check for up1, got: %q", output)
	}
	if !strings.Contains(output, "8 PASS, 0 WARN, 0 FAIL") {
		t.Errorf("expected all checks to pass when scoped to the reachable up1 context, got: %q", output)
	}
}

// TestDoctorCmd_NoManifest_FallsBackToActiveContext confirms doctor keeps
// working with no manifest present (pre-existing behavior) and notes that
// manifest contexts were not checked.
func TestDoctorCmd_NoManifest_FallsBackToActiveContext(t *testing.T) {
	defer withHealthyDoctorStub(t)()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	// Point --manifest at a path that doesn't exist so manifest loading fails
	// deterministically regardless of the test runner's working directory.
	root.SetArgs([]string{"doctor", "--manifest", filepath.Join(t.TempDir(), "missing.yml")})

	if err := root.Execute(); err != nil {
		t.Fatalf("doctor command failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, `[context] Active context reachable — "default"`) {
		t.Errorf("expected fallback single-context check, got: %q", output)
	}
	if !strings.Contains(output, "manifest contexts were not checked") {
		t.Errorf("expected a note that manifest contexts were not checked, got: %q", output)
	}
}
