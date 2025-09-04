package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeDockerStub creates a stub 'docker' script that simulates outputs used by the CLI.
func writeDockerStub(t *testing.T, dir string) string {
	t.Helper()
	stub := `#!/bin/sh
# Collect subcommand
cmd="$1"; shift
case "$cmd" in
  volume)
    sub="$1"; shift
	    if [ "$sub" = "ls" ]; then
	      # Simulate orphan volume only since no filesets use volumes in basic config
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
    # passthrough args; detect --services, ps, up, or config --hash
    # Handle services listing
    for a in "$@"; do [ "$a" = "--services" ] && { echo "nginx"; exit 0; }; done
    # Handle hash: output "service hash"
    if [ "$1" = "config" ] && [ "$2" = "--hash" ]; then
      svc="$3"
      echo "$svc deadbeefcafebabe"
      exit 0
    fi
    # Handle ps json
    if [ "$1" = "ps" ] && [ "$2" = "--format" ] && [ "$3" = "json" ]; then
      echo "[]"
      exit 0
    fi
    # Handle up -d
    if [ "$1" = "up" ] && [ "$2" = "-d" ]; then
      exit 0
    fi
    # Fallback: empty success
    exit 0
    ;;
  ps)
    # docker ps -a --format ... used by ListComposeContainersAll
    exit 0
    ;;
  inspect)
    # Not used in these tests
    echo "{}"
    exit 0
    ;;
esac
exit 0
`
	path := filepath.Join(dir, "docker")
	if runtime.GOOS == "windows" {
		path += ".cmd"
	}
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

func withStubDocker(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	stub := writeDockerStub(t, dir)
	_ = stub
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return func() { _ = os.Setenv("PATH", oldPath) }
}

// basicConfigPath creates a minimal valid dockform config and file layout
// in a temporary directory suitable for CLI tests without external filesets.
func basicConfigPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	appRoot := filepath.Join(dir, "website")
	if err := os.MkdirAll(appRoot, 0o755); err != nil {
		t.Fatalf("mkdir app root: %v", err)
	}
	// Minimal compose file required by validator
	composePath := filepath.Join(appRoot, "docker-compose.yaml")
	if err := os.WriteFile(composePath, []byte("version: '3'\nservices: {}\n"), 0o644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	// Minimal config referencing the app and declaring networks
	cfg := strings.Join([]string{
		"docker:",
		"  context: default",
		"  identifier: demo",
		"applications:",
		"  website:",
		"    root: website",
		"    files:",
		"      - docker-compose.yaml",
		"networks:",
		"  demo-network: {}",
	}, "\n") + "\n"
	cfgPath := filepath.Join(dir, "dockform.yml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}
