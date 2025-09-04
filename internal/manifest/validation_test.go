package manifest

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
)

func TestNormalize_DefaultsEnvMergingAndFiles(t *testing.T) {
	base := t.TempDir()
	cfg := Config{
		Docker:      DockerConfig{Identifier: "id"},
		Environment: &Environment{Files: []string{"global.env", "root/vars.env", "global.env"}},
		Applications: map[string]Application{
			"web": {
				Root:        "app", // relative, should resolve
				Environment: &Environment{Files: []string{"app.env"}},
				EnvFile:     []string{"compose.env"},
			},
		},
	}
	if err := cfg.normalizeAndValidate(base); err != nil {
		t.Fatalf("normalizeAndValidate: %v", err)
	}
	if cfg.Docker.Context != "default" {
		t.Fatalf("expected default docker.context, got %q", cfg.Docker.Context)
	}
	app := cfg.Applications["web"]
	wantRoot := filepath.Clean(filepath.Join(base, "app"))
	if app.Root != wantRoot {
		t.Fatalf("root not resolved: want %q got %q", wantRoot, app.Root)
	}
	// Root env files rebased relative to app root, then app env, then app EnvFile; de-duped
	wantEnv := []string{"../global.env", "../root/vars.env", "app.env", "compose.env"}
	if !reflect.DeepEqual(app.EnvFile, wantEnv) {
		t.Fatalf("env files mismatch:\nwant: %#v\n got: %#v", wantEnv, app.EnvFile)
	}
	// When Files empty, default docker-compose.yml under resolved root
	wantFiles := []string{filepath.Join(wantRoot, "docker-compose.yml")}
	if !reflect.DeepEqual(app.Files, wantFiles) {
		t.Fatalf("files mismatch:\nwant: %#v\n got: %#v", wantFiles, app.Files)
	}
}

func TestNormalize_InvalidApplicationKey(t *testing.T) {
	cfg := Config{
		Docker:       DockerConfig{Identifier: "x"},
		Applications: map[string]Application{"Bad Name": {Root: "/tmp"}},
	}
	if err := cfg.normalizeAndValidate("/base"); err == nil {
		t.Fatalf("expected error for invalid app key")
	} else if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

func TestNormalize_MissingIdentifier(t *testing.T) {
	cfg := Config{Docker: DockerConfig{}, Applications: map[string]Application{"ok": {Root: "/tmp"}}}
	if err := cfg.normalizeAndValidate("/base"); err == nil {
		t.Fatalf("expected error for missing identifier")
	} else if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

func TestNormalize_InlineEnvLastWins(t *testing.T) {
	base := t.TempDir()
	cfg := Config{
		Docker:      DockerConfig{Identifier: "id"},
		Environment: &Environment{Inline: []string{"FOO=A", "BAR=1", "BAR=1"}},
		Applications: map[string]Application{
			"web": {Root: "app", Environment: &Environment{Inline: []string{"BAR=2", "BAZ=3"}}},
		},
	}
	if err := cfg.normalizeAndValidate(base); err != nil {
		t.Fatalf("normalizeAndValidate: %v", err)
	}
	got := cfg.Applications["web"].EnvInline
	want := []string{"FOO=A", "BAR=2", "BAZ=3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inline env mismatch:\nwant: %#v\n got: %#v", want, got)
	}
}

func TestNormalize_SopsMergingAndValidation(t *testing.T) {
	base := t.TempDir()
	// valid case
	cfg := Config{
		Docker:  DockerConfig{Identifier: "id"},
		Secrets: &Secrets{Sops: []string{"secrets/root.env", "  ", "secrets/another.env"}},
		Applications: map[string]Application{
			"web": {Root: "app", Secrets: &Secrets{Sops: []string{"app.env"}}},
		},
	}
	if err := cfg.normalizeAndValidate(base); err != nil {
		t.Fatalf("normalizeAndValidate: %v", err)
	}
	sops := cfg.Applications["web"].SopsSecrets
	want := []string{"../secrets/root.env", "../secrets/another.env", "app.env"}
	if !reflect.DeepEqual(sops, want) {
		t.Fatalf("sops merged mismatch:\nwant: %#v\n got: %#v", want, sops)
	}

	// invalid root-level extension
	cfg2 := Config{
		Docker:  DockerConfig{Identifier: "id"},
		Secrets: &Secrets{Sops: []string{"secrets/root.txt"}},
		Applications: map[string]Application{
			"web": {Root: "app"},
		},
	}
	if err := cfg2.normalizeAndValidate(base); err == nil {
		t.Fatalf("expected error for invalid root sops extension")
	} else if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}

	// invalid app-level extension
	cfg3 := Config{
		Docker: DockerConfig{Identifier: "id"},
		Applications: map[string]Application{
			"web": {Root: "app", Secrets: &Secrets{Sops: []string{"bad.txt"}}},
		},
	}
	if err := cfg3.normalizeAndValidate(base); err == nil {
		t.Fatalf("expected error for invalid app sops extension")
	} else if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

func TestFilesets_ValidationAndNormalization(t *testing.T) {
	base := t.TempDir()
	cfg := Config{
		Docker: DockerConfig{Identifier: "id"},
		Filesets: map[string]FilesetSpec{
			"code": {Source: "src", TargetVolume: "data", TargetPath: "/app"},
		},
	}
	if err := cfg.normalizeAndValidate(base); err != nil {
		t.Fatalf("normalizeAndValidate: %v", err)
	}
	fs := cfg.Filesets["code"]
	wantAbs := filepath.Clean(filepath.Join(base, "src"))
	if fs.SourceAbs != wantAbs {
		t.Fatalf("SourceAbs mismatch: want %q got %q", wantAbs, fs.SourceAbs)
	}

	// target path not absolute
	cfgRel := Config{Docker: DockerConfig{Identifier: "id"}, Filesets: map[string]FilesetSpec{"x": {Source: "s", TargetVolume: "data", TargetPath: "rel"}}}
	if err := cfgRel.normalizeAndValidate(base); err == nil {
		t.Fatalf("expected error for relative target path")
	} else if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}

	// target path is /
	cfgRoot := Config{Docker: DockerConfig{Identifier: "id"}, Filesets: map[string]FilesetSpec{"x": {Source: "s", TargetVolume: "data", TargetPath: "/"}}}
	if err := cfgRoot.normalizeAndValidate(base); err == nil {
		t.Fatalf("expected error for target path '/'")
	} else if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}

	// missing source
	cfgNoSrc := Config{Docker: DockerConfig{Identifier: "id"}, Filesets: map[string]FilesetSpec{"x": {Source: "", TargetVolume: "data", TargetPath: "/p"}}}
	if err := cfgNoSrc.normalizeAndValidate(base); err == nil {
		t.Fatalf("expected error for missing source")
	} else if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}

	// invalid fileset key
	cfgBadKey := Config{Docker: DockerConfig{Identifier: "id"}, Filesets: map[string]FilesetSpec{"Bad Key": {Source: "s", TargetVolume: "data", TargetPath: "/p"}}}
	if err := cfgBadKey.normalizeAndValidate(base); err == nil {
		t.Fatalf("expected error for invalid fileset key")
	} else if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}
