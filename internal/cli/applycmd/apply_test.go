package applycmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/clitest"
)

func TestApply_PrintsPlan_WhenRemovalsPresent(t *testing.T) {
	t.Helper()
	defer clitest.WithStubDocker(t)()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("yes\n"))
	root.SetArgs([]string{"apply", "--manifest", clitest.BasicConfigPath(t)})

	if err := root.Execute(); err != nil {
		t.Fatalf("apply execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "× ") && !strings.Contains(got, " will be deleted") {
		t.Fatalf("expected delete lines in apply plan; got: %s", got)
	}
}

// TestApply_PrintsFullPlanDetail_NotJustSummary guards dockform-ltv: apply must
// print the full plan detail (section bodies) for review, not only the summary
// line. Regression risk is re-routing the plan through the rolling-log TUI, which
// clips tall plans and leaves only the trailing summary visible.
func TestApply_PrintsFullPlanDetail_NotJustSummary(t *testing.T) {
	defer clitest.WithStubDocker(t)()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("no\n")) // decline so we only exercise the plan review
	root.SetArgs([]string{"apply", "--manifest", clitest.BasicConfigPath(t)})

	if err := root.Execute(); err != nil {
		t.Fatalf("apply execute: %v", err)
	}
	got := out.String()

	// The summary line alone is not enough — the detail above it must be present.
	if !strings.Contains(got, "Plan:") {
		t.Fatalf("expected plan summary line; got: %s", got)
	}
	// Detail: at least one resource section body must be rendered alongside the summary.
	hasDetail := strings.Contains(got, " will be deleted") ||
		strings.Contains(got, " will be created") ||
		strings.Contains(got, "up-to-date")
	if !hasDetail {
		t.Fatalf("expected full plan detail alongside summary, not just the summary line; got: %s", got)
	}
}

func TestApply_NoRemovals_NoGuidance(t *testing.T) {
	t.Helper()
	undo := clitest.WithCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then exit 0; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then exit 0; fi ;;
  compose)
    for a in "$@"; do [ "$a" = "--services" ] && { echo "nginx"; exit 0; }; done
    prev=""
    for a in "$@"; do
      if [ "$prev" = "--hash" ]; then svc="$a"; echo "$svc deadbeef"; exit 0; fi
      prev="$a"
    done
    saw_ps=0
    saw_format=0
    saw_json=0
    for a in "$@"; do
      [ "$a" = "ps" ] && saw_ps=1
      [ "$a" = "--format" ] && saw_format=1
      [ "$a" = "json" ] && saw_json=1
    done
    if [ "$saw_ps" = "1" ] && [ "$saw_format" = "1" ] && [ "$saw_json" = "1" ]; then echo "[]"; exit 0; fi
    saw_up=0
    saw_detach=0
    for a in "$@"; do
      [ "$a" = "up" ] && saw_up=1
      [ "$a" = "-d" ] && saw_detach=1
    done
    if [ "$saw_up" = "1" ] && [ "$saw_detach" = "1" ]; then exit 0; fi
    exit 0 ;;
  ps)
    exit 0 ;;
  inspect)
    echo "{}"; exit 0 ;;
 esac
 exit 0
`)
	defer undo()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("yes\n"))
	root.SetArgs([]string{"apply", "--manifest", clitest.BasicConfigPath(t)})

	if err := root.Execute(); err != nil {
		t.Fatalf("apply execute: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "× ") || strings.Contains(got, " will be deleted") {
		t.Fatalf("did not expect any delete lines; got: %s", got)
	}
}

func TestApply_InvalidConfigPath_ReturnsError(t *testing.T) {
	t.Helper()
	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("yes\n"))
	root.SetArgs([]string{"apply", "--manifest", "does-not-exist.yml"})

	if err := root.Execute(); err == nil {
		t.Fatalf("expected error for invalid config path, got nil")
	}
}

func TestApply_PropagatesApplyError_OnDockerFailure(t *testing.T) {
	t.Helper()
	undo := clitest.WithCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "boom" 1>&2; exit 1; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "demo-network"; exit 0; fi ;;
  compose)
    for a in "$@"; do [ "$a" = "--services" ] && { echo "nginx"; exit 0; }; done
    prev=""
    for a in "$@"; do
      if [ "$prev" = "--hash" ]; then svc="$a"; echo "$svc deadbeef"; exit 0; fi
      prev="$a"
    done
    saw_ps=0
    saw_format=0
    saw_json=0
    for a in "$@"; do
      [ "$a" = "ps" ] && saw_ps=1
      [ "$a" = "--format" ] && saw_format=1
      [ "$a" = "json" ] && saw_json=1
    done
    if [ "$saw_ps" = "1" ] && [ "$saw_format" = "1" ] && [ "$saw_json" = "1" ]; then echo "[]"; exit 0; fi
    saw_up=0
    saw_detach=0
    for a in "$@"; do
      [ "$a" = "up" ] && saw_up=1
      [ "$a" = "-d" ] && saw_detach=1
    done
    if [ "$saw_up" = "1" ] && [ "$saw_detach" = "1" ]; then exit 0; fi
    exit 0 ;;
  ps)
    exit 0 ;;
  inspect)
    echo "{}"; exit 0 ;;
 esac
 exit 0
`)
	defer undo()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("yes\n"))
	root.SetArgs([]string{"apply", "--manifest", clitest.BasicConfigPath(t)})

	if err := root.Execute(); err == nil {
		t.Fatalf("expected error from apply when docker fails, got nil")
	} else if !strings.Contains(err.Error(), "discover existing docker resources") {
		t.Fatalf("expected error to mention docker resource discovery; got: %v", err)
	}
}

func TestApply_SkipConfirmation_BypassesPrompt(t *testing.T) {
	t.Helper()
	defer clitest.WithStubDocker(t)()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"apply", "--skip-confirmation", "--manifest", clitest.BasicConfigPath(t)})

	if err := root.Execute(); err != nil {
		t.Fatalf("apply execute with --skip-confirmation: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "Type yes to confirm") || strings.Contains(got, "Answer:") {
		t.Fatalf("expected no confirmation prompt in output; got: %s", got)
	}
	if strings.Contains(got, "canceled") {
		t.Fatalf("did not expect apply to be canceled when skipping confirmation; got: %s", got)
	}
	// Regression guard for Finding I7: the apply progress view (plain here,
	// since root.SetOut gives cmd.OutOrStdout() a non-TTY *bytes.Buffer) must
	// actually reach the command's writer. Before the fix, RunOrPlain/RunPlain
	// hardcoded os.Stdout, so this text never showed up in `out` even though
	// apply had genuinely run and finished — precisely how a stack reporting
	// its services as "not applied" shipped invisible to the test suite.
	if !strings.Contains(got, "Applying") || !strings.Contains(got, "changes applied") {
		t.Fatalf("expected apply's progress output on the command's writer; got: %s", got)
	}
}

func TestApply_PruneErrors_NonStrictByDefault(t *testing.T) {
	t.Helper()
	undo := clitest.WithCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "orphan-vol"; exit 0; fi
    if [ "$sub" = "rm" ]; then echo "volume remove failed" 1>&2; exit 1; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then exit 0; fi ;;
  compose)
    for a in "$@"; do [ "$a" = "--services" ] && { echo "nginx"; exit 0; }; done
    prev=""
    for a in "$@"; do
      if [ "$prev" = "--hash" ]; then svc="$a"; echo "$svc deadbeef"; exit 0; fi
      prev="$a"
    done
    saw_ps=0
    saw_format=0
    saw_json=0
    for a in "$@"; do
      [ "$a" = "ps" ] && saw_ps=1
      [ "$a" = "--format" ] && saw_format=1
      [ "$a" = "json" ] && saw_json=1
    done
    if [ "$saw_ps" = "1" ] && [ "$saw_format" = "1" ] && [ "$saw_json" = "1" ]; then echo "[]"; exit 0; fi
    saw_up=0
    saw_detach=0
    for a in "$@"; do
      [ "$a" = "up" ] && saw_up=1
      [ "$a" = "-d" ] && saw_detach=1
    done
    if [ "$saw_up" = "1" ] && [ "$saw_detach" = "1" ]; then exit 0; fi
    exit 0 ;;
  ps)
    exit 0 ;;
  inspect)
    echo "{}"; exit 0 ;;
esac
exit 0
`)
	defer undo()

	root := cli.TestNewRootCmd()
	root.SetIn(strings.NewReader("yes\n"))
	root.SetArgs([]string{"apply", "--skip-confirmation", "--manifest", clitest.BasicConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("expected apply to succeed in non-strict prune mode, got: %v", err)
	}
}

func TestApply_StrictPrune_FailsOnPruneErrors(t *testing.T) {
	t.Helper()
	undo := clitest.WithCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "orphan-vol"; exit 0; fi
    if [ "$sub" = "rm" ]; then echo "volume remove failed" 1>&2; exit 1; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then exit 0; fi ;;
  compose)
    for a in "$@"; do [ "$a" = "--services" ] && { echo "nginx"; exit 0; }; done
    prev=""
    for a in "$@"; do
      if [ "$prev" = "--hash" ]; then svc="$a"; echo "$svc deadbeef"; exit 0; fi
      prev="$a"
    done
    saw_ps=0
    saw_format=0
    saw_json=0
    for a in "$@"; do
      [ "$a" = "ps" ] && saw_ps=1
      [ "$a" = "--format" ] && saw_format=1
      [ "$a" = "json" ] && saw_json=1
    done
    if [ "$saw_ps" = "1" ] && [ "$saw_format" = "1" ] && [ "$saw_json" = "1" ]; then echo "[]"; exit 0; fi
    saw_up=0
    saw_detach=0
    for a in "$@"; do
      [ "$a" = "up" ] && saw_up=1
      [ "$a" = "-d" ] && saw_detach=1
    done
    if [ "$saw_up" = "1" ] && [ "$saw_detach" = "1" ]; then exit 0; fi
    exit 0 ;;
  ps)
    exit 0 ;;
  inspect)
    echo "{}"; exit 0 ;;
esac
exit 0
`)
	defer undo()

	root := cli.TestNewRootCmd()
	root.SetIn(strings.NewReader("yes\n"))
	root.SetArgs([]string{"apply", "--skip-confirmation", "--strict-prune", "--manifest", clitest.BasicConfigPath(t)})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected apply to fail when --strict-prune is set")
	}
	if !strings.Contains(err.Error(), "prune") {
		t.Fatalf("expected prune-related error, got: %v", err)
	}
}

// --log-file names the destination; it must not ALSO write the default run log.
// Silently keeping two copies of every run, one at a path the user never chose,
// is the behaviour this overrides.
func TestApply_LogFileOverridesTheDefaultRunLog(t *testing.T) {
	defer clitest.WithStubDocker(t)()

	manifest := clitest.BasicConfigPath(t)
	logFile := filepath.Join(t.TempDir(), "explicit.log")

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"apply", "--skip-confirmation", "--manifest", manifest, "--log-file", logFile})
	if err := root.Execute(); err != nil {
		t.Fatalf("apply with --log-file: %v", err)
	}

	if _, err := os.Stat(logFile); err != nil {
		t.Fatalf("the log file named by --log-file was not written: %v", err)
	}
	dotDockform := filepath.Join(filepath.Dir(manifest), ".dockform")
	if _, err := os.Stat(dotDockform); err == nil {
		t.Fatalf("--log-file was given, but %s was created too — the run log should be overridden, not duplicated", dotDockform)
	}
	if got := out.String(); !strings.Contains(got, logFile) {
		t.Errorf("expected the reported log path to be the one given to --log-file; got: %s", got)
	}
}
