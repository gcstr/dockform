package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
)

func TestResolveConfigPath_DirectoryPreferenceOrder(t *testing.T) {
	dir := t.TempDir()
	// Both files exist; resolver should pick dockform.yml first
	yamlPath := filepath.Join(dir, "dockform.yaml")
	ymlPath := filepath.Join(dir, "dockform.yml")
	if err := os.WriteFile(yamlPath, []byte("docker:\n  identifier: x\n"), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	if err := os.WriteFile(ymlPath, []byte("docker:\n  identifier: x\n"), 0o644); err != nil {
		t.Fatalf("write yml: %v", err)
	}

	got, err := resolveConfigPath(dir)
	if err != nil {
		t.Fatalf("resolveConfigPath: %v", err)
	}
	if got != ymlPath {
		t.Fatalf("expected %q, got %q", ymlPath, got)
	}
}

func TestResolveConfigPath_DirectoryNoFiles(t *testing.T) {
	dir := t.TempDir()
	_, err := resolveConfigPath(dir)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !apperr.IsKind(err, apperr.NotFound) {
		t.Fatalf("expected NotFound kind, got: %v", err)
	}
}

func TestResolveConfigPath_FileGiven(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dockform.yml")
	if err := os.WriteFile(path, []byte("docker:\n  identifier: x\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := resolveConfigPath(path)
	if err != nil {
		t.Fatalf("resolveConfigPath(file): %v", err)
	}
	if got != path {
		t.Fatalf("expected same file path, got %q", got)
	}
}

func TestResolveConfigPath_NonExistentPathReturned(t *testing.T) {
	// Should return the path as-is to allow higher level to fail on read
	bogus := filepath.Join(t.TempDir(), "does-not-exist.yml")
	got, err := resolveConfigPath(bogus)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != bogus {
		t.Fatalf("expected path to be returned unchanged; got %q", got)
	}
}

func TestResolveConfigPath_EmptyPathUsesCWD(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	defer func() { _ = os.Chdir(prev) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	path := filepath.Join(dir, "dockform.yml")
	if err := os.WriteFile(path, []byte("docker:\n  identifier: x\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := resolveConfigPath("")
	if err != nil {
		t.Fatalf("resolveConfigPath(cwd): %v", err)
	}
	// Resolve potential macOS /private symlink differences
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(path)
	if gotResolved != wantResolved {
		t.Fatalf("expected %q, got %q", wantResolved, gotResolved)
	}
}

func TestLoadWithWarnings_SetsBaseDirAndReportsMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dockform.yml")
	content := "docker:\n  identifier: myapp\n  context: ${CTX}\nstacks:\n  web:\n    root: website\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	cfg, missing, err := LoadWithWarnings(path)
	if err != nil {
		t.Fatalf("LoadWithWarnings: %v", err)
	}
	if cfg.BaseDir != dir {
		t.Fatalf("BaseDir mismatch: %q", cfg.BaseDir)
	}
	if cfg.Docker.Context != "default" {
		t.Fatalf("expected default docker.context, got %q", cfg.Docker.Context)
	}
	stack := cfg.Stacks["web"]
	expectedRoot := filepath.Clean(filepath.Join(dir, "website"))
	if stack.Root != expectedRoot {
		t.Fatalf("stack root not resolved; want %q got %q", expectedRoot, stack.Root)
	}
	if len(missing) != 1 || missing[0] != "CTX" {
		t.Fatalf("expected missing [CTX], got %#v", missing)
	}
}

func TestLoadWithWarnings_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dockform.yml")
	bad := "docker:\n  identifier: [\n"
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	_, _, err := LoadWithWarnings(path)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got: %v", err)
	}
}

func TestRenderWithWarningsAndPath_ReturnsFilenameAndContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-config.yml")
	t.Setenv("TEST_VAR", "test-value")
	content := "docker:\n  context: ${TEST_VAR}\n  identifier: myapp\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	out, filename, missing, err := RenderWithWarningsAndPath(path)
	if err != nil {
		t.Fatalf("RenderWithWarningsAndPath: %v", err)
	}

	// Check filename is a relative path that ends with the base name
	if !strings.HasSuffix(filename, "test-config.yml") {
		t.Fatalf("expected filename to end with 'test-config.yml', got %q", filename)
	}

	// Check content is interpolated
	if !strings.Contains(out, "context: test-value") {
		t.Fatalf("expected interpolated content, got: %q", out)
	}

	// Check no missing vars
	if len(missing) != 0 {
		t.Fatalf("expected no missing vars, got: %v", missing)
	}
}

func TestRenderWithWarningsAndPath_ReportsMissingVars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dockform.yml")
	// Ensure MISSING_VAR is not set
	if err := os.Unsetenv("MISSING_VAR"); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}
	content := "docker:\n  context: ${MISSING_VAR}\n  identifier: myapp\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	out, filename, missing, err := RenderWithWarningsAndPath(path)
	if err != nil {
		t.Fatalf("RenderWithWarningsAndPath: %v", err)
	}

	// Check filename is a relative path that ends with the base name
	if !strings.HasSuffix(filename, "dockform.yml") {
		t.Fatalf("expected filename to end with 'dockform.yml', got %q", filename)
	}

	// Check missing var is reported
	if len(missing) != 1 || missing[0] != "MISSING_VAR" {
		t.Fatalf("expected missing [MISSING_VAR], got: %v", missing)
	}

	// Check content has empty replacement
	if !strings.Contains(out, "context: \n") {
		t.Fatalf("expected empty replacement for missing var, got: %q", out)
	}
}

func TestRenderWithWarningsAndPath_DirectoryResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dockform.yml")
	content := "docker:\n  identifier: myapp\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Pass directory instead of file
	out, filename, missing, err := RenderWithWarningsAndPath(dir)
	if err != nil {
		t.Fatalf("RenderWithWarningsAndPath: %v", err)
	}

	// Should resolve to dockform.yml (relative path)
	if !strings.HasSuffix(filename, "dockform.yml") {
		t.Fatalf("expected filename to end with 'dockform.yml', got %q", filename)
	}

	if !strings.Contains(out, "identifier: myapp") {
		t.Fatalf("expected content from resolved file, got: %q", out)
	}

	if len(missing) != 0 {
		t.Fatalf("expected no missing vars, got: %v", missing)
	}
}

func TestRenderWithWarningsAndPath_NonExistentFile(t *testing.T) {
	bogusPath := filepath.Join(t.TempDir(), "does-not-exist.yml")

	_, _, _, err := RenderWithWarningsAndPath(bogusPath)
	if err == nil {
		t.Fatalf("expected error for non-existent file, got nil")
	}
	if !apperr.IsKind(err, apperr.NotFound) {
		t.Fatalf("expected NotFound error, got: %v", err)
	}
}
