package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestDestroy_ShowsPlan_WhenResourcesPresent(t *testing.T) {
	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "app-volume"; exit 0; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "app-network"; exit 0; fi ;;
  ps)
    # docker ps -a --format ... used by ListComposeContainersAll
    echo "test-project;web;test-web-1"
    exit 0 ;;
  inspect)
    echo "{}"
    exit 0 ;;
esac
exit 0
`)
	defer undo()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("demo\n")) // Provide identifier for confirmation
	root.SetArgs([]string{"destroy", "-c", basicConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("destroy execute: %v", err)
	}
	got := out.String()

	// Should show plan with resources in the standard plan format
	// The output should show Stacks, Networks, and Volumes sections
	if !strings.Contains(got, "Stacks") || !strings.Contains(got, "test-project") {
		t.Fatalf("expected Stacks section with project; got: %s", got)
	}
	if !strings.Contains(got, "web") && !strings.Contains(got, "will be destroyed") {
		t.Fatalf("expected service 'web' to be listed for destruction; got: %s", got)
	}
	if !strings.Contains(got, "Networks") {
		t.Fatalf("expected Networks section; got: %s", got)
	}
	if !strings.Contains(got, "app-network") {
		t.Fatalf("expected network 'app-network' in plan; got: %s", got)
	}
	if !strings.Contains(got, "Volumes") {
		t.Fatalf("expected Volumes section; got: %s", got)
	}
	if !strings.Contains(got, "app-volume") {
		t.Fatalf("expected volume 'app-volume' in plan; got: %s", got)
	}
}

func TestDestroy_NoResources_ShowsNoResourcesMessage(t *testing.T) {
	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then exit 0; fi ;;  # Empty output
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then exit 0; fi ;;  # Empty output
  ps)
    # docker ps -a returns empty
    exit 0 ;;
  inspect)
    echo "{}"
    exit 0 ;;
esac
exit 0
`)
	defer undo()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"destroy", "-c", basicConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("destroy execute: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "No managed resources found to destroy.") {
		t.Fatalf("expected no resources message; got: %s", got)
	}
}

func TestDestroy_InvalidIdentifier_CancelsDestroy(t *testing.T) {
	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "app-volume"; exit 0; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "app-network"; exit 0; fi ;;
  ps)
    echo "test-project;web;test-web-1"
    exit 0 ;;
  inspect)
    echo "{}"
    exit 0 ;;
esac
exit 0
`)
	defer undo()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("wrong-identifier\n")) // Wrong identifier
	root.SetArgs([]string{"destroy", "-c", basicConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("destroy execute: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, " canceled") {
		t.Fatalf("expected destruction to be canceled; got: %s", got)
	}
	// Should not contain destruction progress
	if strings.Contains(got, "Destroying") {
		t.Fatalf("did not expect destruction to proceed; got: %s", got)
	}
}

func TestDestroy_CorrectIdentifier_ProceedsWithDestruction(t *testing.T) {
	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "app-volume"; exit 0; fi 
    if [ "$sub" = "rm" ]; then exit 0; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "app-network"; exit 0; fi 
    if [ "$sub" = "rm" ]; then exit 0; fi ;;
  container)
    sub="$1"; shift
    if [ "$sub" = "rm" ]; then exit 0; fi ;;
  ps)
    echo "test-project;web;test-web-1"
    exit 0 ;;
  inspect)
    echo "{}"
    exit 0 ;;
esac
exit 0
`)
	defer undo()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("demo\n")) // Correct identifier
	root.SetArgs([]string{"destroy", "-c", basicConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("destroy execute: %v", err)
	}
	got := out.String()

	// Should not be canceled
	if strings.Contains(got, " canceled") {
		t.Fatalf("did not expect destruction to be canceled; got: %s", got)
	}
	// Should complete successfully (no error, no cancellation)
	// Note: Progress bar output may not appear in test environment
}

func TestDestroy_SkipConfirmation_BypassesPrompt(t *testing.T) {
	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "app-volume"; exit 0; fi 
    if [ "$sub" = "rm" ]; then exit 0; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "app-network"; exit 0; fi 
    if [ "$sub" = "rm" ]; then exit 0; fi ;;
  container)
    sub="$1"; shift
    if [ "$sub" = "rm" ]; then exit 0; fi ;;
  ps)
    echo "test-project;web;test-web-1"
    exit 0 ;;
  inspect)
    echo "{}"
    exit 0 ;;
esac
exit 0
`)
	defer undo()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	// No stdin provided; should not prompt when flag is set.
	root.SetArgs([]string{"destroy", "--skip-confirmation", "-c", basicConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("destroy execute with --skip-confirmation: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "Type the identifier name") || strings.Contains(got, "Answer:") {
		t.Fatalf("expected no confirmation prompt in output; got: %s", got)
	}
	if strings.Contains(got, "canceled") {
		t.Fatalf("did not expect destroy to be canceled when skipping confirmation; got: %s", got)
	}
	// Should complete successfully (no error, no cancellation)
	// Note: Progress bar output may not appear in test environment
}

func TestDestroy_InvalidConfigPath_ReturnsError(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("demo\n"))
	root.SetArgs([]string{"destroy", "-c", "does-not-exist.yml"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error for invalid config path, got nil")
	}
}

func TestDestroy_DockerDiscoveryFailure_ReturnsError(t *testing.T) {
	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "boom" 1>&2; exit 1; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "app-network"; exit 0; fi ;;
  ps)
    exit 0 ;;
  inspect)
    echo "{}"
    exit 0 ;;
esac
exit 0
`)
	defer undo()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("demo\n"))
	root.SetArgs([]string{"destroy", "-c", basicConfigPath(t)})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error from destroy when docker discovery fails, got nil")
	}
}
