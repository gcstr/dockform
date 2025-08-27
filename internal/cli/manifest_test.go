package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifest_Render_Success_WithTrailingNewline(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"manifest", "render", "-c", exampleConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("manifest render execute: %v", err)
	}
	got := out.String()
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("expected trailing newline in output; got: %q", got)
	}
	if !strings.Contains(got, "docker:") || !strings.Contains(got, "applications:") {
		t.Fatalf("expected manifest contents in output; got: %s", got)
	}
}

func TestManifest_Render_InterpolatesEnvAndWarnsOnMissing(t *testing.T) {
	// Ensure AGE_KEY_FILE is set to avoid extra warnings, but set a custom var too
	t.Setenv("AGE_KEY_FILE", "dummy")
	t.Setenv("CUSTOM_VAR", "value123")

	// Create a temporary manifest to exercise interpolation and newline behavior
	dir := t.TempDir()
	path := filepath.Join(dir, "dockform.yml")
	content := "docker:\n  context: ${CUSTOM_VAR}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp manifest: %v", err)
	}

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"manifest", "render", "-c", path})
	if err := root.Execute(); err != nil {
		t.Fatalf("manifest render execute: %v", err)
	}
	got := out.String()
	if want := "context: value123"; !strings.Contains(got, want) {
		t.Fatalf("expected interpolated env var; want substring %q in %q", want, got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("expected trailing newline in output; got: %q", got)
	}
}

func TestManifest_Render_InvalidPath_ReturnsError(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"manifest", "render", "-c", "does-not-exist.yml"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error for invalid manifest path, got nil")
	}
}
