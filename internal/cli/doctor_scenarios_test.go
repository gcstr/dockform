package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDoctorCmd_AllHealthy(t *testing.T) {
	defer withHealthyDoctorStub(t)()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor"})

	if err := root.Execute(); err != nil {
		t.Fatalf("doctor command failed: %v", err)
	}

	output := out.String()

	// Check header
	if !strings.Contains(output, "Doctor — health scan") {
		t.Errorf("missing header, got: %q", output)
	}
	if !strings.Contains(output, "Context: default") {
		t.Errorf("missing context info, got: %q", output)
	}

	// Check all expected checks are present
	requiredChecks := []string{"[engine]", "[context]", "[compose]", "[sops]", "[gpg]", "[helper]", "[net-perms]", "[vol-perms]"}
	for _, check := range requiredChecks {
		if !strings.Contains(output, check) {
			t.Errorf("missing check %q in output: %q", check, output)
		}
	}

	// Check summary
	if !strings.Contains(output, "Summary: 8 checks") {
		t.Errorf("missing summary line, got: %q", output)
	}
	if !strings.Contains(output, "8 PASS, 0 WARN, 0 FAIL") {
		t.Errorf("expected all pass, got: %q", output)
	}
	if !strings.Contains(output, "All good!") {
		t.Errorf("expected 'All good!' message, got: %q", output)
	}
	if !strings.Contains(output, "exit code 0") {
		t.Errorf("expected exit code 0, got: %q", output)
	}
}

func TestDoctorCmd_WithContext(t *testing.T) {
	defer withHealthyDoctorStub(t)()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor", "--context", "mycontext"})

	if err := root.Execute(); err != nil {
		t.Fatalf("doctor command failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Context: mycontext") {
		t.Errorf("expected custom context in output, got: %q", output)
	}
}

func TestDoctorCmd_EngineUnreachable(t *testing.T) {
	defer withDoctorStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "connection refused" 1>&2
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
`)()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor"})

	err := root.Execute()
	// Should exit with error
	if err == nil {
		t.Fatalf("expected error when engine is unreachable")
	}

	output := out.String()
	// Check for failure marker
	if !strings.Contains(output, "×") && !strings.Contains(output, "[engine]") {
		t.Errorf("expected engine failure marker, got: %q", output)
	}
	// Check summary shows failures
	if !strings.Contains(output, "FAIL") {
		t.Errorf("expected FAIL in summary, got: %q", output)
	}
	if !strings.Contains(output, "exit code 1") {
		t.Errorf("expected exit code 1, got: %q", output)
	}
}

func TestDoctorCmd_ComposeNotFound(t *testing.T) {
	defer withDoctorStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "20.0.0"
    exit 0
    ;;
  context)
    echo '"unix:///var/run/docker.sock"'
    exit 0
    ;;
  compose)
    echo "compose: command not found" 1>&2
    exit 1
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
`)()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error when compose is missing")
	}

	output := out.String()
	if !strings.Contains(output, "[compose]") {
		t.Errorf("missing compose check, got: %q", output)
	}
	if !strings.Contains(output, "not found") {
		t.Errorf("expected 'not found' for compose, got: %q", output)
	}
	// Should show remedy
	if !strings.Contains(output, "Remedy:") || !strings.Contains(output, "Install docker compose plugin") {
		t.Errorf("expected remedy for compose, got: %q", output)
	}
}

func TestDoctorCmd_SopsWarning(t *testing.T) {
	// Create a stub with docker but without sops
	stubScript := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "20.0.0"
    exit 0
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
	dir := t.TempDir()
	dockerPath := filepath.Join(dir, "docker")
	if runtime.GOOS == "windows" {
		dockerPath += ".cmd"
	}
	if err := os.WriteFile(dockerPath, []byte(stubScript), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}

	// Set PATH to only include our stub dir (no sops)
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	defer func() { _ = os.Setenv("PATH", oldPath) }()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor"})

	err := root.Execute()
	// Should complete with warning exit code
	if err == nil {
		t.Fatalf("expected error (exit code 2) when sops missing")
	}

	output := out.String()
	// Check for warning marker
	if !strings.Contains(output, "[sops]") {
		t.Errorf("missing sops check, got: %q", output)
	}
	if !strings.Contains(output, "!") && !strings.Contains(output, "WARN") {
		t.Errorf("expected warning marker, got: %q", output)
	}
	// Check summary
	if !strings.Contains(output, "WARN") {
		t.Errorf("expected WARN in summary, got: %q", output)
	}
	if !strings.Contains(output, "exit code 2") {
		t.Errorf("expected exit code 2 for warnings, got: %q", output)
	}
}

func TestDoctorCmd_NetworkPermissionFailed(t *testing.T) {
	defer withDoctorStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "20.0.0"
    exit 0
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
    if [ "$sub" = "create" ]; then
      echo "permission denied" 1>&2
      exit 1
    fi
    ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "create" ]; then exit 0; fi
    if [ "$sub" = "rm" ]; then exit 0; fi
    ;;
esac
exit 0
`)()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error when network create fails")
	}

	output := out.String()
	if !strings.Contains(output, "[net-perms]") {
		t.Errorf("missing net-perms check, got: %q", output)
	}
	if !strings.Contains(output, "Cannot create network") {
		t.Errorf("expected network error message, got: %q", output)
	}
	// Should show remedy with pipe prefix
	if !strings.Contains(output, "│") {
		t.Errorf("expected pipe prefix for indented remedy, got: %q", output)
	}
	if !strings.Contains(output, "Remedy:") {
		t.Errorf("expected remedy line, got: %q", output)
	}
}

func TestDoctorCmd_VolumePermissionFailed(t *testing.T) {
	defer withDoctorStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "20.0.0"
    exit 0
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
    if [ "$sub" = "create" ]; then
      echo "permission denied on /var/lib/docker/volumes" 1>&2
      exit 1
    fi
    ;;
esac
exit 0
`)()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error when volume create fails")
	}

	output := out.String()
	if !strings.Contains(output, "[vol-perms]") {
		t.Errorf("missing vol-perms check, got: %q", output)
	}
	if !strings.Contains(output, "Cannot create volume") {
		t.Errorf("expected volume error message, got: %q", output)
	}
}

func TestDoctorCmd_HelperImageMissing(t *testing.T) {
	defer withDoctorStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "20.0.0"
    exit 0
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
    # image inspect fails when image is missing
    exit 1
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
`)()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor"})

	err := root.Execute()
	// Helper missing is a warning, should exit with code 2
	if err == nil {
		t.Fatalf("expected error (exit code 2) when helper image missing")
	}

	output := out.String()
	if !strings.Contains(output, "[helper]") {
		t.Errorf("missing helper check, got: %q", output)
	}
	// Should be a warning, not a failure
	if !strings.Contains(output, "WARN") {
		t.Errorf("expected WARN for missing helper, got: %q", output)
	}
}

func TestDoctorCmd_IndentedOutput(t *testing.T) {
	defer withDoctorStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "connection refused to socket" 1>&2
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
`)()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor"})

	_ = root.Execute()
	output := out.String()

	// Check that error messages are indented with pipe prefix
	lines := strings.Split(output, "\n")
	foundIndented := false
	for _, line := range lines {
		if strings.HasPrefix(line, "│     ") {
			foundIndented = true
			break
		}
	}
	if !foundIndented {
		t.Errorf("expected indented lines with pipe prefix, got: %q", output)
	}
}
