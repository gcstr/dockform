package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_NormalizesAndMerges(t *testing.T) {
	dir := t.TempDir()
	// layout
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}
	mustWrite(t, filepath.Join(appDir, "docker-compose.yml"), "version: '3'\nservices: {}\n")
	mustWrite(t, filepath.Join(appDir, "app.env"), "")
	mustWrite(t, filepath.Join(dir, "root.env"), "")
	mustWrite(t, filepath.Join(dir, "rootsecret.env"), "FOO=bar\n")

	yml := strings.Join([]string{
		"docker:",
		"  identifier: test-id",
		"environment:",
		"  files:",
		"    - root.env",
		"  inline:",
		"    - BAR=1",
		"    - FOO=1",
		"secrets:",
		"  sops:",
		"    - rootsecret.env",
		"applications:",
		"  web:",
		"    root: app",
		"    environment:",
		"      files:",
		"        - app.env",
		"      inline:",
		"        - FOO=2",
		"volumes:",
		"  v1: {}",
		"filesets:",
		"  files:",
		"    source: app",
		"    target_volume: v1",
		"    target_path: /assets",
	}, "\n") + "\n"
	cfgPath := filepath.Join(dir, "dockform.yml")
	mustWrite(t, cfgPath, yml)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Docker.Context != "default" {
		t.Fatalf("expected default docker.context, got %q", cfg.Docker.Context)
	}
	app, ok := cfg.Applications["web"]
	if !ok {
		t.Fatalf("missing app web")
	}
	// Root resolved to absolute app dir
	if app.Root != appDir {
		t.Fatalf("app root not resolved: %q != %q", app.Root, appDir)
	}
	// Env files: root rebased to app (../root.env) then app.env
	want0 := "../root.env"
	if len(app.EnvFile) < 2 || app.EnvFile[0] != want0 || app.EnvFile[1] != "app.env" {
		t.Fatalf("unexpected env files: %#v", app.EnvFile)
	}
	// Inline env: BAR=1 preserved, FOO dedupbed to last (2)
	if len(app.EnvInline) != 2 || app.EnvInline[0] != "BAR=1" || app.EnvInline[1] != "FOO=2" {
		t.Fatalf("unexpected inline env: %#v", app.EnvInline)
	}
	// SOPS secrets merged and rebased to app root
	if len(app.SopsSecrets) != 1 || app.SopsSecrets[0] != "../rootsecret.env" {
		t.Fatalf("unexpected sops secrets: %#v", app.SopsSecrets)
	}
	// Fileset source resolved to absolute path
	a, ok := cfg.Filesets["files"]
	if !ok {
		t.Fatalf("missing filesets.files")
	}
	if a.SourceAbs != appDir {
		t.Fatalf("fileset SourceAbs not resolved: %q != %q", a.SourceAbs, appDir)
	}
}

func TestRender_InterpolatesEnv(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "dockform.yml"), "docker:\n  identifier: test-id\nvalue: ${MY_VAR}\n")
	t.Setenv("MY_VAR", "abc")
	out, err := Render(dir)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "value: abc") {
		t.Fatalf("render output mismatch: %q", out)
	}
}

func TestLoad_DirectoryResolution(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "dockform.yml"), "docker:\n  identifier: test-id\napplications: {}\n")
	if _, err := Load(dir); err != nil {
		t.Fatalf("load dir: %v", err)
	}
	// Empty path discovers file in cwd
	old, _ := os.Getwd()
	defer func() { _ = os.Chdir(old) }()
	_ = os.Chdir(dir)
	if _, err := Load(""); err != nil {
		t.Fatalf("load from cwd: %v", err)
	}
}

func TestAssets_ValidationErrors(t *testing.T) {
	dir := t.TempDir()
	// target_volume not declared under volumes
	yml := "docker:\n  identifier: test-id\nfilesets:\n  bad:\n    source: .\n    target_volume: missing\n    target_path: /data\n"
	mustWrite(t, filepath.Join(dir, "dockform.yml"), yml)
	if _, err := Load(dir); err == nil {
		t.Fatalf("expected error for missing target_volume")
	}
	// target_path must be absolute
	yml2 := "docker:\n  identifier: test-id\nvolumes:\n  v: {}\nfilesets:\n  bad:\n    source: .\n    target_volume: v\n    target_path: data\n"
	mustWrite(t, filepath.Join(dir, "dockform.yml"), yml2)
	if _, err := Load(dir); err == nil {
		t.Fatalf("expected error for non-absolute target_path")
	}
}

func TestApplication_DefaultComposeFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	appDir := filepath.Join(dir, "srv")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWrite(t, filepath.Join(appDir, "docker-compose.yml"), "version: '3'\nservices: {}\n")
	yml := "docker:\n  identifier: test-id\napplications:\n  a:\n    root: srv\n"
	mustWrite(t, filepath.Join(dir, "dockform.yml"), yml)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	app := cfg.Applications["a"]
	if len(app.Files) != 1 || app.Files[0] != filepath.Join(appDir, "docker-compose.yml") {
		t.Fatalf("unexpected default compose file: %#v", app.Files)
	}
}

func TestApplication_InvalidKey_Error(t *testing.T) {
	dir := t.TempDir()
	yml := "docker:\n  identifier: test-id\napplications:\n  INVALID: {root: .}\n"
	mustWrite(t, filepath.Join(dir, "dockform.yml"), yml)
	if _, err := Load(dir); err == nil {
		t.Fatalf("expected error for invalid application key")
	}
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
