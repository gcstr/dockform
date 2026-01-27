package common

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

func TestFindManifestFilesRespectsDepth(t *testing.T) {
	dir := t.TempDir()
	deeper := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(deeper, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	allowed := filepath.Join(dir, "dockform.yml")
	if err := os.WriteFile(allowed, []byte("docker: {}"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	skipped := filepath.Join(deeper, "dockform.yaml")
	if err := os.WriteFile(skipped, []byte("docker: {}"), 0o644); err != nil {
		t.Fatalf("write deep manifest: %v", err)
	}
	files, err := findManifestFiles(dir, 1)
	if err != nil {
		t.Fatalf("findManifestFiles: %v", err)
	}
	if len(files) != 1 || files[0] != allowed {
		t.Fatalf("expected only top-level manifest, got: %v", files)
	}
}

func TestReadDaemonContextLabels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dockform.yml")
	content := "daemons:\n  prod:\n    context: prod-context\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if got := readDaemonContextLabels(path); got != "prod" {
		t.Fatalf("expected daemon name 'prod', got %q", got)
	}
	if got := readDaemonContextLabels(filepath.Join(t.TempDir(), "missing.yml")); got != "" {
		t.Fatalf("expected empty context for missing file, got %q", got)
	}
}

func TestDisplayDaemonInfoDefaults(t *testing.T) {
	var out bytes.Buffer
	pr := ui.StdPrinter{Out: &out}
	cfg := &manifest.Config{
		Daemons: map[string]manifest.DaemonConfig{
			"default": {Context: "", Identifier: "demo"},
		},
	}
	DisplayDaemonInfo(pr, cfg)
	got := ui.StripANSI(out.String())
	if !bytes.Contains([]byte(got), []byte("Context: default")) {
		t.Fatalf("expected default context fallback, got: %q", got)
	}
	if !bytes.Contains([]byte(got), []byte("Identifier: demo")) {
		t.Fatalf("expected identifier output, got: %q", got)
	}
}

func TestMaskSecretsSimpleStrategies(t *testing.T) {
	stack := manifest.Stack{}
	yaml := "password: hunter2\napi_key: 123456\nnotes: token=abcdef"
	full := MaskSecretsSimple(yaml, stack, "full")
	if strings.Contains(full, "hunter2") {
		t.Fatalf("expected password to be masked in full strategy")
	}
	partial := MaskSecretsSimple(yaml, stack, "partial")
	if !strings.Contains(partial, "hu***r2") {
		t.Fatalf("expected partial mask to preserve edges, got: %s", partial)
	}
	preserve := MaskSecretsSimple(yaml, stack, "preserve-length")
	if !strings.Contains(preserve, "******") {
		t.Fatalf("expected preserve-length mask of same length, got: %s", preserve)
	}
}

func TestGetConfirmationNonTTYYes(t *testing.T) {
	cmd := &cobra.Command{}
	var in bytes.Buffer
	in.WriteString("yes\n")
	var out bytes.Buffer
	cmd.SetIn(&in)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	pr := ui.StdPrinter{Out: &out, Err: &out}
	ok, err := GetConfirmation(cmd, pr, ConfirmationOptions{})
	if err != nil {
		t.Fatalf("confirmation error: %v", err)
	}
	if !ok {
		t.Fatalf("expected confirmation to succeed for yes input")
	}
}

func TestGetDestroyConfirmationNonTTY(t *testing.T) {
	cmd := &cobra.Command{}
	var in bytes.Buffer
	in.WriteString("demo\n")
	var out bytes.Buffer
	cmd.SetIn(&in)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	pr := ui.StdPrinter{Out: &out, Err: &out}
	ok, err := GetDestroyConfirmation(cmd, pr, DestroyConfirmationOptions{Identifier: "demo"})
	if err != nil {
		t.Fatalf("destroy confirmation error: %v", err)
	}
	if !ok {
		t.Fatalf("expected destroy confirmation to accept matching identifier")
	}
}
