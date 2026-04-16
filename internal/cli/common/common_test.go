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
	content := "identifier: test\ncontexts:\n  prod: {}\n"
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

func TestDisplayDaemonInfoSingleContext(t *testing.T) {
	var out bytes.Buffer
	pr := ui.StdPrinter{Out: &out}
	cfg := &manifest.Config{
		Identifier: "demo",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
	}
	DisplayDaemonInfo(pr, cfg)
	got := ui.StripANSI(out.String())
	if !strings.Contains(got, "Identifier:") {
		t.Fatalf("expected 'Identifier:' label, got: %q", got)
	}
	if !strings.Contains(got, "demo") {
		t.Fatalf("expected identifier value 'demo', got: %q", got)
	}
	// Singular label when only one context
	if !strings.Contains(got, "Context:") {
		t.Fatalf("expected singular 'Context:' label, got: %q", got)
	}
	if strings.Contains(got, "Contexts:") {
		t.Fatalf("expected singular label but got plural 'Contexts:', got: %q", got)
	}
	if !strings.Contains(got, "default") {
		t.Fatalf("expected context name 'default', got: %q", got)
	}
}

func TestDisplayDaemonInfoMultipleContexts(t *testing.T) {
	var out bytes.Buffer
	pr := ui.StdPrinter{Out: &out}
	cfg := &manifest.Config{
		Identifier: "homeserver",
		Contexts: map[string]manifest.ContextConfig{
			"hetzner-one":   {},
			"hetzner-two":   {},
			"hetzner-three": {},
		},
	}
	DisplayDaemonInfo(pr, cfg)
	got := ui.StripANSI(out.String())
	// Plural label when multiple contexts
	if !strings.Contains(got, "Contexts:") {
		t.Fatalf("expected plural 'Contexts:' label, got: %q", got)
	}
	// All context names present
	for _, name := range []string{"hetzner-one", "hetzner-two", "hetzner-three"} {
		if !strings.Contains(got, name) {
			t.Fatalf("expected context %q in output, got: %q", name, got)
		}
	}
	// Middle-dot separator present
	if !strings.Contains(got, "·") {
		t.Fatalf("expected middle-dot separator between contexts, got: %q", got)
	}
	// Identifier first: its line must appear before the contexts line
	idxID := strings.Index(got, "Identifier:")
	idxCtx := strings.Index(got, "Contexts:")
	if idxID < 0 || idxCtx < 0 {
		t.Fatalf("both labels must be present, got: %q", got)
	}
	if idxID > idxCtx {
		t.Fatalf("Identifier: must appear before Contexts:, got: %q", got)
	}
}

func TestDisplayDaemonInfoNoIdentifier(t *testing.T) {
	var out bytes.Buffer
	pr := ui.StdPrinter{Out: &out}
	cfg := &manifest.Config{
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
	}
	DisplayDaemonInfo(pr, cfg)
	got := ui.StripANSI(out.String())
	if strings.Contains(got, "Identifier:") {
		t.Fatalf("expected no Identifier line when identifier is empty, got: %q", got)
	}
	if !strings.Contains(got, "Context:") {
		t.Fatalf("expected 'Context:' label, got: %q", got)
	}
}

func TestDisplayDaemonInfoNoContexts(t *testing.T) {
	var out bytes.Buffer
	pr := ui.StdPrinter{Out: &out}
	cfg := &manifest.Config{
		Identifier: "demo",
		Contexts:   map[string]manifest.ContextConfig{},
	}
	DisplayDaemonInfo(pr, cfg)
	got := ui.StripANSI(out.String())
	if !strings.Contains(got, "No contexts configured") {
		t.Fatalf("expected fallback message, got: %q", got)
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
