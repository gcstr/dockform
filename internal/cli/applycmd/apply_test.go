package applycmd_test

import (
	"bytes"
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
