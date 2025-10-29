package doctorcmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli/doctorcmd"
)

func TestPrintIndentedLines_WordWrap(t *testing.T) {
	var buf bytes.Buffer
	longText := "This is a very long text that should be wrapped properly when printed because it exceeds the maximum width of eighty characters per line and needs to be split"
	doctorcmd.PrintIndentedLines(&buf, longText)

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have multiple lines
	if len(lines) < 2 {
		t.Errorf("expected text to wrap into multiple lines, got: %q", output)
	}

	// All lines should start with pipe prefix
	for i, line := range lines {
		if !strings.HasPrefix(line, "│     ") {
			t.Errorf("line %d missing pipe prefix: %q", i, line)
		}
		// Each line should not exceed reasonable width
		if len(line) > 85 {
			t.Errorf("line %d too long (%d chars): %q", i, len(line), line)
		}
	}
}

func TestPrintIndentedLines_EmptyText(t *testing.T) {
	var buf bytes.Buffer
	doctorcmd.PrintIndentedLines(&buf, "")
	if buf.Len() != 0 {
		t.Errorf("expected no output for empty text, got: %q", buf.String())
	}

	buf.Reset()
	doctorcmd.PrintIndentedLines(&buf, "   ")
	if buf.Len() != 0 {
		t.Errorf("expected no output for whitespace-only text, got: %q", buf.String())
	}
}

func TestCheckStatus_Constants(t *testing.T) {
	// Ensure status constants are distinct
	if doctorcmd.StatusPass == doctorcmd.StatusWarn || doctorcmd.StatusPass == doctorcmd.StatusFail || doctorcmd.StatusWarn == doctorcmd.StatusFail {
		t.Errorf("status constants should be distinct: pass=%d warn=%d fail=%d", doctorcmd.StatusPass, doctorcmd.StatusWarn, doctorcmd.StatusFail)
	}
}

// withHealthyDoctorStub creates a stub docker command that simulates a healthy system
func withHealthyDoctorStub(t *testing.T) func() {
	t.Helper()
	var stub string
	if runtime.GOOS == "windows" {
		stub = `@echo off
if "%1"=="version" (
  echo 20.0.0
  exit /b 0
)
if "%1"=="context" (
  echo "unix:///var/run/docker.sock"
  exit /b 0
)
if "%1"=="compose" (
  echo 2.29.0
  exit /b 0
)
if "%1"=="image" (
  exit /b 0
)
if "%1"=="network" (
  if "%2"=="create" exit /b 0
  if "%2"=="rm" exit /b 0
)
if "%1"=="volume" (
  if "%2"=="create" exit /b 0
  if "%2"=="rm" exit /b 0
)
exit /b 0
`
	} else {
		stub = `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    # docker version --format {{.Server.Version}}
    echo "20.0.0"
    exit 0
    ;;
  context)
    # docker context inspect default --format {{json .Endpoints.docker.Host}}
    echo '"unix:///var/run/docker.sock"'
    exit 0
    ;;
  compose)
    # docker compose version --short
    echo "2.29.0"
    exit 0
    ;;
  image)
    # docker image inspect alpine:3.22
    exit 0
    ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "create" ]; then
      # docker network create
      exit 0
    elif [ "$sub" = "rm" ]; then
      # docker network rm
      exit 0
    fi
    ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "create" ]; then
      # docker volume create
      exit 0
    elif [ "$sub" = "rm" ]; then
      # docker volume rm
      exit 0
    fi
    ;;
esac
exit 0
`
	}
	return withDoctorStub(t, stub)
}

// withDoctorStub creates a custom docker stub for doctor tests
func withDoctorStub(t *testing.T, script string) func() {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	if runtime.GOOS == "windows" {
		path += ".cmd"
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}

	// Also create a fake sops in the same directory
	var sopsStub string
	sopsPath := filepath.Join(dir, "sops")
	if runtime.GOOS == "windows" {
		sopsPath += ".cmd"
		sopsStub = `@echo off
if "%1"=="--version" (
  echo sops 3.10.2
  exit /b 0
)
exit /b 0
`
	} else {
		sopsStub = `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "sops 3.10.2"
  exit 0
fi
exit 0
`
	}
	if err := os.WriteFile(sopsPath, []byte(sopsStub), 0o755); err != nil {
		t.Fatalf("write sops stub: %v", err)
	}

	// Provide minimal gpg stub so doctor reports pass by default
	var gpgStub string
	gpgPath := filepath.Join(dir, "gpg")
	if runtime.GOOS == "windows" {
		gpgPath += ".cmd"
		gpgStub = `@echo off
if "%1"=="--version" (
  echo gpg (GnuPG) 2.4.3
  exit /b 0
)
if "%1"=="--help" (
  echo Usage: gpg [options]
  echo   --pinentry-mode
  exit /b 0
)
exit /b 0
`
	} else {
		gpgStub = `#!/bin/sh
case "$1" in
  --version)
    echo "gpg (GnuPG) 2.4.3"
    exit 0
    ;;
  --help)
    echo "Usage: gpg [options]"
    echo "  --pinentry-mode"
    exit 0
    ;;
esac
exit 0
`
	}
	if err := os.WriteFile(gpgPath, []byte(gpgStub), 0o755); err != nil {
		t.Fatalf("write gpg stub: %v", err)
	}

	// Provide gpgconf stub for agent socket info
	var gpgConfStub string
	gpgConfPath := filepath.Join(dir, "gpgconf")
	if runtime.GOOS == "windows" {
		gpgConfPath += ".cmd"
		gpgConfStub = `@echo off
if "%1"=="--list-dirs" if "%2"=="agent-socket" (
  echo /tmp/gpg-agent.sock
  exit /b 0
)
exit /b 0
`
	} else {
		gpgConfStub = `#!/bin/sh
if [ "$1" = "--list-dirs" ] && [ "$2" = "agent-socket" ]; then
  echo "/tmp/gpg-agent.sock"
  exit 0
fi
exit 0
`
	}
	if err := os.WriteFile(gpgConfPath, []byte(gpgConfStub), 0o755); err != nil {
		t.Fatalf("write gpgconf stub: %v", err)
	}

	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return func() { _ = os.Setenv("PATH", oldPath) }
}
