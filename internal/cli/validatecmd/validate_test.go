package validatecmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/clitest"
)

func TestValidate_Success_PrintsMessage(t *testing.T) {
	t.Helper()
	defer clitest.WithStubDocker(t)()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"validate", "-c", clitest.BasicConfigPath(t)})

	if err := root.Execute(); err != nil {
		t.Fatalf("validate execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Context:") || !strings.Contains(got, "Identifier:") {
		t.Fatalf("expected context/identifier in output, got: %q", got)
	}
	if !strings.Contains(got, "validation successful") {
		t.Fatalf("expected validation success message in output, got: %q", got)
	}
}

func TestValidate_InvalidConfigPath_ReturnsError(t *testing.T) {
	t.Helper()
	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"validate", "-c", "does-not-exist.yml"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error for invalid config path, got nil")
	}
}

func TestValidate_DockerNotReachable_ReturnsError(t *testing.T) {
	t.Helper()
	undo := clitest.WithCustomDockerStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "boom" 1>&2; exit 1 ;;
  *)
    exit 0 ;;
esac
`)
	defer undo()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"validate", "-c", clitest.BasicConfigPath(t)})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected docker unreachable error, got nil")
	}
}

func TestValidate_SopsKeyFileMissing_ReturnsError(t *testing.T) {
	t.Helper()
	defer clitest.WithStubDocker(t)()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	cfg := filepath.Join(t.TempDir(), "cfg.yml")
	content := "sops:\n  age:\n    key_file: /no/such/key\n"
	writeFile(t, cfg, content)

	root.SetArgs([]string{"validate", "-c", cfg})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error for missing sops key file, got nil")
	}
}

func writeFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
