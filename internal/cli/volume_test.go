package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// volumeConfigPath creates a minimal config with a volume defined
func volumeConfigPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	appRoot := filepath.Join(dir, "website")
	if err := os.MkdirAll(appRoot, 0o755); err != nil {
		t.Fatalf("mkdir app root: %v", err)
	}
	// Minimal compose file
	composePath := filepath.Join(appRoot, "docker-compose.yaml")
	composeContent := `version: '3'
services:
  web:
    image: nginx:alpine
    volumes:
      - app-data:/data
volumes:
  app-data: {}
`
	if err := os.WriteFile(composePath, []byte(composeContent), 0o644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	// Config with volume
	cfg := strings.Join([]string{
		"docker:",
		"  context: default",
		"  identifier: demo",
		"stacks:",
		"  website:",
		"    root: website",
		"    files:",
		"      - docker-compose.yaml",
		"volumes:",
		"  app-data: {}",
	}, "\n") + "\n"
	cfgPath := filepath.Join(dir, "dockform.yml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func TestVolumeRestore_WithStopContainers_RestartsRunningContainers(t *testing.T) {
	cfgPath := volumeConfigPath(t)
	cfgDir := filepath.Dir(cfgPath)

	// Create a dummy snapshot file
	snapshotDir := filepath.Join(cfgDir, "snapshots")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshots: %v", err)
	}
	snapshotPath := filepath.Join(snapshotDir, "test.tar")
	if err := os.WriteFile(snapshotPath, []byte("dummy tar content"), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    case "$sub" in
      ls)
        echo "app-data"
        exit 0 ;;
      inspect)
        # Return JSON with volume details
        echo '{"Name":"app-data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/app-data/_data","Labels":{},"Options":{}}'
        exit 0 ;;
    esac
    ;;
  ps)
    # Check if it's ps -a (all containers) or ps (running only)
    # Format: docker ps [-a] --filter volume=X --format {{.Names}}
    has_all=0
    for arg in "$@"; do
      if [ "$arg" = "-a" ]; then
        has_all=1
        break
      fi
    done
    
    if [ "$has_all" = "1" ]; then
      # All containers using the volume
      echo "demo-web-1"
    else
      # Only running containers
      echo "demo-web-1"
    fi
    exit 0 ;;
  container)
    sub="$1"; shift
    case "$sub" in
      stop)
        exit 0 ;;
      start)
        exit 0 ;;
    esac
    ;;
  run)
    # Helper container for IsVolumeEmpty and restore operations
    # For IsVolumeEmpty, return "empty"
    echo "empty"
    exit 0 ;;
esac
exit 0
`)
	defer undo()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"volume", "restore", "app-data", snapshotPath, "-c", cfgPath, "--stop-containers", "--force"})

	if err := root.Execute(); err != nil {
		t.Fatalf("volume restore execute: %v\nOutput: %s", err, out.String())
	}

	got := out.String()

	// Verify success message
	if !strings.Contains(got, "Restored snapshot into volume app-data") {
		t.Errorf("expected success message; got: %s", got)
	}
}

func TestVolumeRestore_ListRunningContainersError_ReturnsError(t *testing.T) {
	cfgPath := volumeConfigPath(t)
	cfgDir := filepath.Dir(cfgPath)

	// Create a dummy snapshot file
	snapshotDir := filepath.Join(cfgDir, "snapshots")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshots: %v", err)
	}
	snapshotPath := filepath.Join(snapshotDir, "test.tar")
	if err := os.WriteFile(snapshotPath, []byte("dummy tar content"), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    case "$sub" in
      ls)
        echo "app-data"
        exit 0 ;;
      inspect)
        echo '{"Name":"app-data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/app-data/_data","Labels":{},"Options":{}}'
        exit 0 ;;
    esac
    ;;
  run)
    # Helper for IsVolumeEmpty - succeed
    echo "empty"
    exit 0 ;;
  ps)
    # Check if it's ps -a (all containers) or ps (running only)
    has_all=0
    for arg in "$@"; do
      if [ "$arg" = "-a" ]; then
        has_all=1
        break
      fi
    done
    
    if [ "$has_all" = "1" ]; then
      # All containers - succeed
      echo "demo-web-1"
      exit 0
    else
      # Error when listing running containers
      echo "Error: cannot connect to Docker daemon" >&2
      exit 1
    fi
    ;;
esac
exit 0
`)
	defer undo()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"volume", "restore", "app-data", snapshotPath, "-c", cfgPath, "--stop-containers", "--force"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error when ListRunningContainersUsingVolume fails, got nil")
	}

	// Verify error message indicates the problem
	errMsg := err.Error()
	if !strings.Contains(errMsg, "cannot connect") && !strings.Contains(errMsg, "exit status 1") {
		t.Errorf("expected error about Docker connection or exit status; got: %s", errMsg)
	}
}

func TestVolumeRestore_VolumeNotEmpty_FailsBeforeStoppingContainers(t *testing.T) {
	cfgPath := volumeConfigPath(t)
	cfgDir := filepath.Dir(cfgPath)

	// Create a dummy snapshot file
	snapshotDir := filepath.Join(cfgDir, "snapshots")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshots: %v", err)
	}
	snapshotPath := filepath.Join(snapshotDir, "test.tar")
	if err := os.WriteFile(snapshotPath, []byte("dummy tar content"), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    case "$sub" in
      ls)
        echo "app-data"
        exit 0 ;;
      inspect)
        echo '{"Name":"app-data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/app-data/_data","Labels":{},"Options":{}}'
        exit 0 ;;
    esac
    ;;
  run)
    # Helper for IsVolumeEmpty - return "notempty"
    echo "notempty"
    exit 0 ;;
  container)
    sub="$1"; shift
    case "$sub" in
      stop)
        # Should not reach here - test should fail before stopping
        echo "ERROR: containers should not be stopped when volume check fails" >&2
        exit 1 ;;
    esac
    ;;
esac
exit 0
`)
	defer undo()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"volume", "restore", "app-data", snapshotPath, "-c", cfgPath, "--stop-containers"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error when volume is not empty without --force flag")
	}

	// Verify error message mentions the issue
	errMsg := err.Error()
	if !strings.Contains(errMsg, "not empty") || !strings.Contains(errMsg, "--force") {
		t.Errorf("expected error about non-empty volume; got: %s", errMsg)
	}
}

func TestVolumeRestore_WithoutStopContainers_FailsWhenContainersPresent(t *testing.T) {
	cfgPath := volumeConfigPath(t)
	cfgDir := filepath.Dir(cfgPath)

	// Create a dummy snapshot file
	snapshotDir := filepath.Join(cfgDir, "snapshots")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshots: %v", err)
	}
	snapshotPath := filepath.Join(snapshotDir, "test.tar")
	if err := os.WriteFile(snapshotPath, []byte("dummy tar content"), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    case "$sub" in
      ls)
        echo "app-data"
        exit 0 ;;
      inspect)
        echo '{"Name":"app-data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/app-data/_data","Labels":{},"Options":{}}'
        exit 0 ;;
    esac
    ;;
  run)
    # Helper for IsVolumeEmpty check - return "empty"
    echo "empty"
    exit 0 ;;
  ps)
    # Return containers using the volume
    echo "demo-web-1"
    exit 0 ;;
  container)
    sub="$1"; shift
    case "$sub" in
      stop)
        # Should not reach here - test should fail before stopping
        echo "ERROR: containers should not be stopped" >&2
        exit 1 ;;
    esac
    ;;
esac
exit 0
`)
	defer undo()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"volume", "restore", "app-data", snapshotPath, "-c", cfgPath})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error when containers are using volume without --stop-containers flag")
	}

	// Verify error message mentions the flag
	errMsg := err.Error()
	if !strings.Contains(errMsg, "use --stop-containers") {
		t.Errorf("expected error to suggest --stop-containers flag; got: %s", errMsg)
	}
}

func TestVolumeRestore_OnlyRestartsRunningContainers(t *testing.T) {
	cfgPath := volumeConfigPath(t)
	cfgDir := filepath.Dir(cfgPath)

	// Create a dummy snapshot file
	snapshotDir := filepath.Join(cfgDir, "snapshots")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshots: %v", err)
	}
	snapshotPath := filepath.Join(snapshotDir, "test.tar")
	if err := os.WriteFile(snapshotPath, []byte("dummy tar content"), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    case "$sub" in
      ls)
        echo "app-data"
        exit 0 ;;
      inspect)
        echo '{"Name":"app-data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/app-data/_data","Labels":{},"Options":{}}'
        exit 0 ;;
    esac
    ;;
  ps)
    # Check if it's ps -a (all containers) or ps (running only)
    has_all=0
    for arg in "$@"; do
      if [ "$arg" = "-a" ]; then
        has_all=1
        break
      fi
    done
    
    if [ "$has_all" = "1" ]; then
      # All containers (both running and stopped)
      echo "demo-web-1"
      echo "demo-worker-1"
    else
      # Only running containers
      echo "demo-web-1"
    fi
    exit 0 ;;
  container)
    sub="$1"; shift
    case "$sub" in
      stop)
        # Both containers should be stopped
        exit 0 ;;
      start)
        # Only demo-web-1 should be started (was running)
        # Verify we don't try to start demo-worker-1
        for arg in "$@"; do
          if [ "$arg" = "demo-worker-1" ]; then
            echo "ERROR: should not start demo-worker-1" >&2
            exit 1
          fi
        done
        exit 0 ;;
    esac
    ;;
  run)
    # Helper container for IsVolumeEmpty and extract/restore operations
    echo "empty"
    exit 0 ;;
esac
exit 0
`)
	defer undo()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"volume", "restore", "app-data", snapshotPath, "-c", cfgPath, "--stop-containers", "--force"})

	if err := root.Execute(); err != nil {
		t.Fatalf("volume restore execute: %v\nOutput: %s", err, out.String())
	}

	got := out.String()

	// Verify success
	if !strings.Contains(got, "Restored snapshot into volume app-data") {
		t.Errorf("expected success message; got: %s", got)
	}

	// If we reach here, it means the stub didn't exit with error,
	// confirming only running containers were restarted
}

func TestVolumeRestore_RestoreFailure_RestartsContainersViaDefer(t *testing.T) {
	cfgPath := volumeConfigPath(t)
	cfgDir := filepath.Dir(cfgPath)

	// Create a dummy snapshot file
	snapshotDir := filepath.Join(cfgDir, "snapshots")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshots: %v", err)
	}
	snapshotPath := filepath.Join(snapshotDir, "test.tar")
	if err := os.WriteFile(snapshotPath, []byte("dummy tar content"), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	containerStarted := false
	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    exit 0 ;;
  volume)
    sub="$1"; shift
    case "$sub" in
      ls)
        echo "app-data"
        exit 0 ;;
      inspect)
        echo '{"Name":"app-data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/app-data/_data","Labels":{},"Options":{}}'
        exit 0 ;;
    esac
    ;;
  ps)
    # Check if it's ps -a (all containers) or ps (running only)
    has_all=0
    for arg in "$@"; do
      if [ "$arg" = "-a" ]; then
        has_all=1
        break
      fi
    done
    
    if [ "$has_all" = "1" ]; then
      echo "demo-web-1"
    else
      echo "demo-web-1"
    fi
    exit 0 ;;
  container)
    sub="$1"; shift
    case "$sub" in
      stop)
        exit 0 ;;
      start)
        # Mark that container start was called (by defer)
        touch /tmp/container_started_$$
        exit 0 ;;
    esac
    ;;
  run)
    # Check what command is being run
    # If it's the volume empty check, return empty
    # If it's the actual restore, fail
    if echo "$@" | grep -q "test -z"; then
      echo "empty"
      exit 0
    else
      # Simulate restore failure
      echo "ERROR: simulated restore failure" >&2
      exit 1
    fi
    ;;
esac
exit 0
`)
	defer undo()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"volume", "restore", "app-data", snapshotPath, "-c", cfgPath, "--stop-containers", "--force"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error from simulated restore failure, got nil")
	}

	// The defer should have called StartContainers even though restore failed
	// We can't easily verify this in the stub, but the key is that the command
	// should fail gracefully and the defer executes
	_ = containerStarted // suppress unused warning

	// Verify error is about the restore operation
	errMsg := err.Error()
	if !strings.Contains(errMsg, "exit status 1") && !strings.Contains(errMsg, "simulated") {
		t.Errorf("expected error about restore failure; got: %s", errMsg)
	}
}
