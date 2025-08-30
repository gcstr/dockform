package validator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gcstr/dockform/internal/config"
	"github.com/gcstr/dockform/internal/dockercli"
)

// writeDockerStub creates a minimal stub 'docker' script that simulates daemon liveness
// for Validate tests.
func writeDockerStub(t *testing.T, dir string) string {
	t.Helper()
	stub := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    # docker version --format ...
    echo "24.0.0"
    exit 0
    ;;
esac
exit 0
`
	path := filepath.Join(dir, "docker")
	if runtime.GOOS == "windows" {
		path += ".cmd"
	}
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

func withStubDocker(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	_ = writeDockerStub(t, dir)
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return func() { _ = os.Setenv("PATH", oldPath) }
}

func withFailingDocker(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	stub := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "boom" 1>&2
    exit 1
    ;;
esac
exit 1
`
	path := filepath.Join(dir, "docker")
	if runtime.GOOS == "windows" {
		path += ".cmd"
	}
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return func() { _ = os.Setenv("PATH", oldPath) }
}

func TestValidate_Succeeds_WithCompleteConfigAndFiles(t *testing.T) {
	defer withStubDocker(t)()
	tmp := t.TempDir()

	// Create example file structure mirroring example/dockform.yml expectations
	mustWrite := func(path string, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	mustWrite(filepath.Join(tmp, "global.env"), "GFOO=bar\n")
	mustWrite(filepath.Join(tmp, "secrets.env"), "S=1\n")
	mustWrite(filepath.Join(tmp, "website", "docker-compose.yaml"), "version: '3'\nservices: {}\n")
	mustWrite(filepath.Join(tmp, "website", "vars.env"), "FOO=bar\n")
	mustWrite(filepath.Join(tmp, "website", "secrets.env"), "S=1\n")

	// Create fake age key file; validator should check existence after ~ expansion
	home := t.TempDir()
	keyFile := filepath.Join(home, ".config", "sops", "age", "keys.txt")
	mustWrite(keyFile, "KEY")
	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", home)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	yml := []byte(`docker:
  context: default
  identifier: test-id
sops:
  age:
    key_file: ~/.config/sops/age/keys.txt
secrets:
  sops:
    - secrets.env
environment:
  files:
    - global.env
applications:
  website:
    root: website
    files:
      - docker-compose.yaml
    environment:
      files:
        - vars.env
    secrets:
      sops:
        - secrets.env
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))

	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	d := dockercli.New(cfg.Docker.Context)
	if err := Validate(context.Background(), cfg, d); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidate_Fails_WhenRootEnvMissing(t *testing.T) {
	defer withStubDocker(t)()
	tmp := t.TempDir()
	mustWrite := func(path string, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// minimal app root and compose to bypass other errors
	mustWrite(filepath.Join(tmp, "website", "docker-compose.yaml"), "version: '3'\nservices: {}\n")
	yml := []byte(`docker:
  context: default
  identifier: test-id
applications:
  website:
    root: website
    files:
      - docker-compose.yaml
environment:
  files:
    - missing.env
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := dockercli.New(cfg.Docker.Context)
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected error for missing root env file")
	}
}

func TestValidate_Fails_WhenRootSopsSecretMissing(t *testing.T) {
	defer withStubDocker(t)()
	tmp := t.TempDir()
	mustWrite := func(path string, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	mustWrite(filepath.Join(tmp, "website", "docker-compose.yaml"), "version: '3'\nservices: {}\n")
	yml := []byte(`docker:
  context: default
  identifier: test-id
secrets:
  sops:
    - secrets.env
applications:
  website:
    root: website
    files:
      - docker-compose.yaml
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := dockercli.New(cfg.Docker.Context)
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected error for missing root sops secret file")
	}
}

func TestValidate_Fails_WhenSopsAgeKeyMissing(t *testing.T) {
	defer withStubDocker(t)()
	tmp := t.TempDir()
	mustWrite := func(path string, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// ensure HOME has no key file path
	home := t.TempDir()
	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", home)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	mustWrite(filepath.Join(tmp, "website", "docker-compose.yaml"), "version: '3'\nservices: {}\n")
	yml := []byte(`docker:
  context: default
  identifier: test-id
sops:
  age:
    key_file: ~/.config/sops/age/keys.txt
applications:
  website:
    root: website
    files:
      - docker-compose.yaml
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := dockercli.New(cfg.Docker.Context)
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected error for missing sops age key file")
	}
}

func TestValidate_Fails_WhenAppRootMissing(t *testing.T) {
	defer withStubDocker(t)()
	tmp := t.TempDir()
	mustWrite := func(path string, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// do not create website dir
	yml := []byte(`docker:
  context: default
  identifier: test-id
applications:
  website:
    root: website
    files:
      - docker-compose.yaml
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := dockercli.New(cfg.Docker.Context)
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected error for missing app root")
	}
}

func TestValidate_Fails_WhenComposeFileMissing(t *testing.T) {
	defer withStubDocker(t)()
	tmp := t.TempDir()
	mustWrite := func(path string, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// create website root only, but no compose file
	if err := os.MkdirAll(filepath.Join(tmp, "website"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yml := []byte(`docker:
  context: default
  identifier: test-id
applications:
  website:
    root: website
    files:
      - docker-compose.yaml
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := dockercli.New(cfg.Docker.Context)
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected error for missing compose file")
	}
}

func TestValidate_Fails_WhenAppEnvMissing(t *testing.T) {
	defer withStubDocker(t)()
	tmp := t.TempDir()
	mustWrite := func(path string, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// create website root and compose
	mustWrite(filepath.Join(tmp, "website", "docker-compose.yaml"), "version: '3'\nservices: {}\n")
	yml := []byte(`docker:
  context: default
  identifier: test-id
applications:
  website:
    root: website
    files:
      - docker-compose.yaml
    environment:
      files:
        - vars.env
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := dockercli.New(cfg.Docker.Context)
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected error for missing app env file")
	}
}

func TestValidate_Fails_WhenAppSopsSecretMissing(t *testing.T) {
	defer withStubDocker(t)()
	tmp := t.TempDir()
	mustWrite := func(path string, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	mustWrite(filepath.Join(tmp, "website", "docker-compose.yaml"), "version: '3'\nservices: {}\n")
	yml := []byte(`docker:
  context: default
  identifier: test-id
applications:
  website:
    root: website
    files:
      - docker-compose.yaml
    secrets:
      sops:
        - secrets.env
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := dockercli.New(cfg.Docker.Context)
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected error for missing app sops secret file")
	}
}

func TestValidate_Identifier_Invalid(t *testing.T) {
	defer withStubDocker(t)()
	tmp := t.TempDir()
	mustWrite := func(path string, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// minimal app root and compose to bypass other errors
	mustWrite(filepath.Join(tmp, "website", "docker-compose.yaml"), "version: '3'\nservices: {}\n")
	yml := []byte(`docker:
  context: default
  identifier: invalid_id!
applications:
  website:
    root: website
    files:
      - docker-compose.yaml
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := dockercli.New(cfg.Docker.Context)
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected identifier validation error")
	}
}

func TestValidate_Fails_WhenDockerDaemonUnreachable(t *testing.T) {
	defer withFailingDocker(t)()
	tmp := t.TempDir()
	// minimal config to hit daemon check first
	if err := os.MkdirAll(filepath.Join(tmp, "website"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yml := []byte(`docker:
  context: default
  identifier: test-id
applications:
  website:
    root: website
    files: []
`)
	if err := os.WriteFile(filepath.Join(tmp, "dockform.yml"), yml, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := dockercli.New(cfg.Docker.Context)
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected docker daemon error")
	}
}

func TestValidate_Assets_SourceRequired(t *testing.T) {
	defer withStubDocker(t)()
	cfg := config.Config{
		Docker:       config.DockerConfig{Context: "default", Identifier: "test-id"},
		Volumes:      map[string]config.TopLevelResourceSpec{"v": {}},
		Networks:     map[string]config.TopLevelResourceSpec{},
		Applications: map[string]config.Application{},
		Filesets:     map[string]config.FilesetSpec{"a": {SourceAbs: "", TargetVolume: "v", TargetPath: "/t"}},
	}
	d := dockercli.New(cfg.Docker.Context)
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected fileset source required error")
	}
}

func TestValidate_Assets_SourceNotFound_AndNotDir(t *testing.T) {
	defer withStubDocker(t)()
	tmp := t.TempDir()
	d := dockercli.New("")
	cfg := config.Config{
		Docker:       config.DockerConfig{Context: "default", Identifier: "test-id"},
		Volumes:      map[string]config.TopLevelResourceSpec{"v": {}},
		Networks:     map[string]config.TopLevelResourceSpec{},
		Applications: map[string]config.Application{},
	}
	// Not found
	cfg.Filesets = map[string]config.FilesetSpec{"a": {SourceAbs: filepath.Join(tmp, "missing"), TargetVolume: "v", TargetPath: "/t"}}
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected fileset not found error")
	}
	// Not a directory: create a regular file
	filePath := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Filesets = map[string]config.FilesetSpec{"a": {SourceAbs: filePath, TargetVolume: "v", TargetPath: "/t"}}
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected fileset not a directory error")
	}
}

func TestValidate_AppRootIsFile_NotDir(t *testing.T) {
	defer withStubDocker(t)()
	tmp := t.TempDir()
	// Create a file at the path intended for app root
	if err := os.WriteFile(filepath.Join(tmp, "website"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	yml := []byte(`docker:
  context: default
  identifier: test-id
applications:
  website:
    root: website
    files: []
`)
	if err := os.WriteFile(filepath.Join(tmp, "dockform.yml"), yml, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := dockercli.New(cfg.Docker.Context)
	if err := Validate(context.Background(), cfg, d); err == nil {
		t.Fatalf("expected app root not directory error")
	}
}
