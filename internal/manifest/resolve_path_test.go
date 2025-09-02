package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
)

func TestResolveConfigPath_ExplicitFile_ReturnsSame(t *testing.T) {
	// Non-existent file path should be returned verbatim and then fail later on read
	got, err := resolveConfigPath("/no/such/file.yml")
	if err != nil {
		t.Fatalf("resolveConfigPath: %v", err)
	}
	if got != "/no/such/file.yml" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestResolveConfigPath_DirectorySearchOrder(t *testing.T) {
	dir := t.TempDir()
	// prefer dockform.yaml over dockform.yml when both exist and searched in order
	// we create only one at a time to assert matching
	pathYaml := filepath.Join(dir, "dockform.yaml")
	if err := os.WriteFile(pathYaml, []byte("docker: {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := resolveConfigPath(dir)
	if err != nil {
		t.Fatalf("resolve dir: %v", err)
	}
	if got != pathYaml {
		t.Fatalf("expected %s, got %s", pathYaml, got)
	}
}

func TestResolveConfigPath_MissingInDir_ReturnsNotFound(t *testing.T) {
	_, err := resolveConfigPath(t.TempDir())
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !apperr.IsKind(err, apperr.NotFound) {
		t.Fatalf("expected NotFound error kind, got: %v", err)
	}
}
