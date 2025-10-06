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
	root.SetArgs([]string{"apply", "-c", clitest.BasicConfigPath(t)})

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
    if [ "$sub" = "ls" ]; then echo "demo-network"; exit 0; fi ;;
  compose)
    for a in "$@"; do [ "$a" = "--services" ] && { echo "nginx"; exit 0; }; done
    if [ "$1" = "config" ] && [ "$2" = "--hash" ]; then svc="$3"; echo "$svc deadbeef"; exit 0; fi
    if [ "$1" = "ps" ] && [ "$2" = "--format" ] && [ "$3" = "json" ]; then echo "[]"; exit 0; fi
    if [ "$1" = "up" ] && [ "$2" = "-d" ]; then exit 0; fi
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
	root.SetArgs([]string{"apply", "-c", clitest.BasicConfigPath(t)})

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
	root.SetArgs([]string{"apply", "-c", "does-not-exist.yml"})

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
    if [ "$1" = "config" ] && [ "$2" = "--hash" ]; then svc="$3"; echo "$svc deadbeef"; exit 0; fi
    if [ "$1" = "ps" ] && [ "$2" = "--format" ] && [ "$3" = "json" ]; then echo "[]"; exit 0; fi
    if [ "$1" = "up" ] && [ "$2" = "-d" ]; then exit 0; fi
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
	root.SetArgs([]string{"apply", "-c", clitest.BasicConfigPath(t)})

	if err := root.Execute(); err == nil {
		t.Fatalf("expected error from apply when docker fails, got nil")
	} else if !strings.Contains(err.Error(), "list volumes") {
		t.Fatalf("expected error to mention list volumes; got: %v", err)
	}
}

func TestApply_SkipConfirmation_BypassesPrompt(t *testing.T) {
	t.Helper()
	defer clitest.WithStubDocker(t)()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"apply", "--skip-confirmation", "-c", clitest.BasicConfigPath(t)})

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
