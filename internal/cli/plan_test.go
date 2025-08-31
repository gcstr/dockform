package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestPlan_PrintsRemovalGuidance_WhenRemovalsPresent_AndNoPrune_Solo(t *testing.T) {
	defer withStubDocker(t)()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plan", "-c", basicConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("plan execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "↓ ") && !strings.Contains(got, " will be removed") {
		t.Fatalf("expected remove lines in plan; got: %s", got)
	}
	if !strings.Contains(got, "No resources will be removed. Include --prune to delete them") {
		t.Fatalf("expected prune guidance; got: %s", got)
	}
}

func TestPlan_DoesNotPrintRemovalGuidance_WhenPruneFlagSet(t *testing.T) {
	defer withStubDocker(t)()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plan", "--prune", "-c", basicConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("plan execute with --prune: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "↓ ") && !strings.Contains(got, " will be removed") {
		t.Fatalf("expected remove lines in plan; got: %s", got)
	}
	if strings.Contains(got, "No resources will be removed. Include --prune to delete them") {
		t.Fatalf("did not expect prune guidance when --prune is set; got: %s", got)
	}
}

func TestPlan_NoRemovals_NoGuidance(t *testing.T) {
	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "demo-volume-1"; exit 0; fi ;;
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

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plan", "-c", basicConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("plan execute: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "↓ ") || strings.Contains(got, " will be removed") {
		t.Fatalf("did not expect any remove lines; got: %s", got)
	}
	if strings.Contains(got, "No resources will be removed. Include --prune to delete them") {
		t.Fatalf("did not expect prune guidance when no removals are present; got: %s", got)
	}
}

func TestPlan_InvalidConfigPath_ReturnsError(t *testing.T) {
	// Clear any env that could affect Render/Load warnings to keep error surface small
	_ = os.Unsetenv("AGE_KEY_FILE")
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plan", "-c", "does-not-exist.yml"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error for invalid config path, got nil")
	}
}
