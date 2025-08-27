package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestValidate_Success_PrintsMessage(t *testing.T) {
	defer withStubDocker(t)()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"validate", "-c", exampleConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("validate execute: %v", err)
	}
	if got := out.String(); got != "validation successful\n" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestValidate_InvalidConfigPath_ReturnsError(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"validate", "-c", "does-not-exist.yml"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error for invalid config path, got nil")
	}
}

func TestValidate_DockerNotReachable_ReturnsError(t *testing.T) {
	undo := withCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "boom" 1>&2; exit 1 ;;
  *)
    exit 0 ;;
esac
`)
	defer undo()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"validate", "-c", exampleConfigPath(t)})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected docker unreachable error, got nil")
	}
}

func TestValidate_SopsKeyFileMissing_ReturnsError(t *testing.T) {
	defer withStubDocker(t)()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	// Minimal config that references a missing SOPS key file
	cfg := filepath.Join(t.TempDir(), "cfg.yml")
	content := "sops:\n  age:\n    key_file: /no/such/key\n"
	writeFile(t, cfg, content)
	root.SetArgs([]string{"validate", "-c", cfg})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error for missing sops key file, got nil")
	}
}

// writeFile is a tiny helper to avoid bringing os import into this test.
func writeFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
