package clitest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteDockerStubProducesScript(t *testing.T) {
	dir := t.TempDir()
	path := WriteDockerStub(t, dir)
	// Ensure script exists and is executable
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected stub script to exist: %v", err)
	}
	// Execute a supported command to verify behaviour
	cmd := exec.Command(path, "volume", "ls")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("expected volume ls stub to succeed: %v", err)
	}
	if strings.TrimSpace(string(out)) != "orphan-vol" {
		t.Fatalf("unexpected stub output: %q", out)
	}
}

func TestWithStubDockerUpdatesAndRestoresPath(t *testing.T) {
	orig := os.Getenv("PATH")
	restore := WithStubDocker(t)
	t.Cleanup(restore)

	newPath := os.Getenv("PATH")
	if newPath == orig {
		t.Fatalf("expected PATH to change after WithStubDocker")
	}
	if !strings.HasSuffix(newPath, orig) {
		t.Fatalf("expected original PATH to be preserved at the end, got: %q", newPath)
	}
	restore()
	if os.Getenv("PATH") != orig {
		t.Fatalf("expected PATH to restore to original value")
	}
}

func TestBasicConfigPathCreatesComposeAndConfig(t *testing.T) {
	cfgPath := BasicConfigPath(t)
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected config file to exist: %v", err)
	}
	content, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !bytes.Contains(content, []byte("stacks:\n  default/website:")) {
		t.Fatalf("expected stack definition in config, got: %s", content)
	}

	rootDir := filepath.Dir(cfgPath)
	composePath := filepath.Join(rootDir, "website", "docker-compose.yaml")
	if _, err := os.Stat(composePath); err != nil {
		t.Fatalf("expected compose file at %s: %v", composePath, err)
	}
}
