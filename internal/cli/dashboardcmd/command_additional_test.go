package dashboardcmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
	"github.com/spf13/cobra"
)

func TestDockerContextName(t *testing.T) {
	if dockerContextName(nil) != "" {
		t.Fatalf("expected empty string for nil config")
	}
	cfg := &manifest.Config{Docker: manifest.DockerConfig{Context: "  demo  "}}
	if dockerContextName(cfg) != "demo" {
		t.Fatalf("expected trimmed context, got %q", dockerContextName(cfg))
	}
}

func TestResolveManifestPathPrefersFlag(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "dockform.yml")
	if err := os.WriteFile(manifestPath, []byte("docker:\n  context: default\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "")
	if err := cmd.Flags().Set("config", manifestPath); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if got := resolveManifestPath(cmd, nil); filepath.Clean(got) != filepath.Clean(manifestPath) {
		t.Fatalf("expected flag value to win, got %q", got)
	}

	cfg := &manifest.Config{BaseDir: dir}
	cmd = &cobra.Command{}
	cmd.Flags().String("config", "", "")
	if got := resolveManifestPath(cmd, cfg); !strings.HasSuffix(got, "dockform.yml") {
		t.Fatalf("expected base dir manifest, got %q", got)
	}
}

func TestManifestPathFromInput(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifestPath := filepath.Join(sub, "dockform.yml")
	if err := os.WriteFile(manifestPath, []byte("docker:\n  context: default\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if got := manifestPathFromInput(manifestPath, dir); filepath.Clean(got) != filepath.Clean(manifestPath) {
		t.Fatalf("expected direct path, got %q", got)
	}
	if got := manifestPathFromInput(sub, dir); filepath.Clean(got) != filepath.Clean(manifestPath) {
		t.Fatalf("expected directory search to find manifest, got %q", got)
	}
	if manifestPathFromInput("", dir) != "" {
		t.Fatalf("expected empty input to return empty path")
	}
}

func TestCandidatePathsAndDedupe(t *testing.T) {
	dir := t.TempDir()
	rel := "config.yml"
	base := filepath.Join(dir, rel)
	paths := candidatePaths(rel, dir)
	seen := map[string]struct{}{}
	for _, p := range paths {
		if _, ok := seen[p]; ok {
			t.Fatalf("candidate paths should be deduped")
		}
		seen[p] = struct{}{}
	}
	if len(dedupePaths([]string{"", base, base})) != 1 {
		t.Fatalf("expected dedupe to remove empty and duplicates")
	}
}

func TestFindManifestInDirAndExpand(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "dockform.yaml")
	if err := os.WriteFile(manifest, []byte("docker:\n  context: default\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if got := findManifestInDir(dir); filepath.Clean(got) != filepath.Clean(manifest) {
		t.Fatalf("expected manifest path, got %q", got)
	}
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	if expandUser("~/file") != filepath.Join(home, "file") {
		t.Fatalf("expected tilde to expand")
	}
}

func TestAbsPath(t *testing.T) {
	if !filepath.IsAbs(absPath(".")) {
		t.Fatalf("expected abs path")
	}
}

func TestNewCommandRunEPropagatesError(t *testing.T) {
	cmd := New()
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatalf("expected dashboard command to fail without config")
	}
}
