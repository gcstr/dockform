package clitest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// WriteDockerStub creates a docker stub script with basic behaviors used across CLI tests.
func WriteDockerStub(t *testing.T, dir string) string {
	t.Helper()

	var stub string
	path := filepath.Join(dir, "docker")

	if runtime.GOOS == "windows" {
		// Windows batch script
		path += ".cmd"
		stub = `@echo off
if "%1"=="volume" (
  if "%2"=="ls" (
    echo orphan-vol
    exit /b 0
  )
)
if "%1"=="network" (
  if "%2"=="ls" (
    echo demo-network
    echo orphan-net
    exit /b 0
  )
)
if "%1"=="compose" (
  for %%a in (%*) do if "%%a"=="--services" (
    echo nginx
    exit /b 0
  )
  if "%2"=="config" if "%3"=="--hash" (
    echo %4 deadbeefcafebabe
    exit /b 0
  )
  if "%2"=="ps" if "%3"=="--format" if "%4"=="json" (
    echo []
    exit /b 0
  )
  if "%2"=="up" if "%3"=="-d" exit /b 0
  exit /b 0
)
if "%1"=="ps" exit /b 0
if "%1"=="inspect" (
  echo {}
  exit /b 0
)
exit /b 0
`
	} else {
		// Unix shell script
		stub = `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then
      echo "orphan-vol"
      exit 0
    fi
    ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then
      echo "demo-network"
      echo "orphan-net"
      exit 0
    fi
    ;;
  compose)
    for a in "$@"; do [ "$a" = "--services" ] && { echo "nginx"; exit 0; }; done
    prev=""
    for a in "$@"; do
      if [ "$prev" = "--hash" ]; then
        svc="$a"
        echo "$svc deadbeefcafebabe"
        exit 0
      fi
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
    if [ "$saw_ps" = "1" ] && [ "$saw_format" = "1" ] && [ "$saw_json" = "1" ]; then
      echo "[]"
      exit 0
    fi
    saw_up=0
    saw_detach=0
    for a in "$@"; do
      [ "$a" = "up" ] && saw_up=1
      [ "$a" = "-d" ] && saw_detach=1
    done
    if [ "$saw_up" = "1" ] && [ "$saw_detach" = "1" ]; then
      exit 0
    fi
    exit 0
    ;;
  ps)
    exit 0
    ;;
  inspect)
    echo "{}"
    exit 0
    ;;
esac
exit 0
`
	}

	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

// WithStubDocker prepends PATH with a standard docker stub used in CLI tests.
func WithStubDocker(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	_ = WriteDockerStub(t, dir)
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return func() { _ = os.Setenv("PATH", oldPath) }
}

// WithCustomDockerStub installs a custom docker stub script and prepends PATH with it.
func WithCustomDockerStub(t *testing.T, script string) func() {
	t.Helper()

	// Skip tests that try to use Unix shell scripts on Windows
	if runtime.GOOS == "windows" && strings.HasPrefix(script, "#!/bin/sh") {
		t.Skip("test uses Unix shell script; skipping on Windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	if runtime.GOOS == "windows" {
		path += ".cmd"
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return func() { _ = os.Setenv("PATH", oldPath) }
}

// BasicConfigPath materialises a minimal Dockform configuration for CLI tests.
func BasicConfigPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	appRoot := filepath.Join(dir, "website")
	if err := os.MkdirAll(appRoot, 0o755); err != nil {
		t.Fatalf("mkdir app root: %v", err)
	}
	composePath := filepath.Join(appRoot, "docker-compose.yaml")
	if err := os.WriteFile(composePath, []byte("version: '3'\nservices: {}\n"), 0o644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	cfg := strings.Join([]string{
		"identifier: demo",
		"contexts:",
		"  default: {}",
		"stacks:",
		"  default/website:",
		"    root: website",
		"    files:",
		"      - docker-compose.yaml",
	}, "\n") + "\n"
	cfgPath := filepath.Join(dir, "dockform.yml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}
