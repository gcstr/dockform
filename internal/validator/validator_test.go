package validator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

// writeDockerStub creates a minimal stub 'docker' script that simulates daemon liveness
// for Validate tests.
func writeDockerStub(t *testing.T, dir string) string {
	t.Helper()
	var stub string
	path := filepath.Join(dir, "docker")
	if runtime.GOOS == "windows" {
		path += ".cmd"
		stub = `@echo off
if "%1"=="version" (
    echo 24.0.0
    exit /b 0
)
exit /b 0
`
	} else {
		stub = `#!/bin/sh
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
	var stub string
	path := filepath.Join(dir, "docker")
	if runtime.GOOS == "windows" {
		path += ".cmd"
		stub = `@echo off
if "%1"=="version" (
    echo boom 1>&2
    exit /b 1
)
exit /b 1
`
	} else {
		stub = `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "boom" 1>&2
    exit 1
    ;;
esac
exit 1
`
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
	mustWrite(filepath.Join(tmp, "website", "docker-compose.yaml"), "version: '3'\nservices: {}\n")
	mustWrite(filepath.Join(tmp, "website", "secrets.env"), "S=1\n")

	// Create fake age key file; validator should check existence after ~ expansion
	home := t.TempDir()
	keyFile := filepath.Join(home, ".config", "sops", "age", "keys.txt")
	mustWrite(keyFile, "KEY")
	// Set home directory environment variable (cross-platform)
	homeEnvVar := "HOME"
	if runtime.GOOS == "windows" {
		homeEnvVar = "USERPROFILE"
	}
	oldHome := os.Getenv(homeEnvVar)
	_ = os.Setenv(homeEnvVar, home)
	t.Cleanup(func() { _ = os.Setenv(homeEnvVar, oldHome) })

	yml := []byte(`identifier: test-id
contexts:
  default: {}
sops:
  age:
    key_file: ~/.config/sops/age/keys.txt
stacks:
  default/website:
    root: website
    files:
      - docker-compose.yaml
    secrets:
      sops:
        - secrets.env
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))

	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	factory := dockercli.NewClientFactory()
	if err := Validate(context.Background(), cfg, factory); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidate_Fails_WhenStackEnvFileMissing(t *testing.T) {
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
	yml := []byte(`identifier: test-id
contexts:
  default: {}
stacks:
  default/website:
    root: website
    files:
      - docker-compose.yaml
    env-file:
      - missing.env
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	factory := dockercli.NewClientFactory()
	if err := Validate(context.Background(), cfg, factory); err == nil {
		t.Fatalf("expected error for missing env file")
	}
}

func TestValidate_Fails_WhenStackSopsSecretMissing(t *testing.T) {
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
	yml := []byte(`identifier: test-id
contexts:
  default: {}
stacks:
  default/website:
    root: website
    files:
      - docker-compose.yaml
    secrets:
      sops:
        - secrets.env
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	factory := dockercli.NewClientFactory()
	if err := Validate(context.Background(), cfg, factory); err == nil {
		t.Fatalf("expected error for missing sops secret file")
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
	// Set home directory environment variable (cross-platform)
	homeEnvVar := "HOME"
	if runtime.GOOS == "windows" {
		homeEnvVar = "USERPROFILE"
	}
	oldHome := os.Getenv(homeEnvVar)
	_ = os.Setenv(homeEnvVar, home)
	t.Cleanup(func() { _ = os.Setenv(homeEnvVar, oldHome) })

	mustWrite(filepath.Join(tmp, "website", "docker-compose.yaml"), "version: '3'\nservices: {}\n")
	mustWrite(filepath.Join(tmp, "secrets.env"), "KEY=value\n")
	yml := []byte(`identifier: test-id
contexts:
  default: {}
sops:
  age:
    key_file: ~/.config/sops/age/keys.txt
stacks:
  default/website:
    root: website
    files:
      - docker-compose.yaml
    secrets:
      sops:
        - secrets.env
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	factory := dockercli.NewClientFactory()
	if err := Validate(context.Background(), cfg, factory); err == nil {
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
	yml := []byte(`identifier: test-id
contexts:
  default: {}
stacks:
  default/website:
    root: website
    files:
      - docker-compose.yaml
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	factory := dockercli.NewClientFactory()
	if err := Validate(context.Background(), cfg, factory); err == nil {
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
	yml := []byte(`identifier: test-id
contexts:
  default: {}
stacks:
  default/website:
    root: website
    files:
      - docker-compose.yaml
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	factory := dockercli.NewClientFactory()
	if err := Validate(context.Background(), cfg, factory); err == nil {
		t.Fatalf("expected error for missing compose file")
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
	yml := []byte(`identifier: invalid_id!
contexts:
  default: {}
stacks:
  default/website:
    root: website
    files:
      - docker-compose.yaml
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))
	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	factory := dockercli.NewClientFactory()
	if err := Validate(context.Background(), cfg, factory); err == nil {
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
	yml := []byte(`identifier: test-id
contexts:
  default: {}
stacks:
  default/website:
    root: website
    files: []
`)
	if err := os.WriteFile(filepath.Join(tmp, "dockform.yml"), yml, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	factory := dockercli.NewClientFactory()
	if err := Validate(context.Background(), cfg, factory); err == nil {
		t.Fatalf("expected docker daemon error")
	}
}

func TestValidate_AppRootIsFile_NotDir(t *testing.T) {
	defer withStubDocker(t)()
	tmp := t.TempDir()
	// Create a file at the path intended for app root
	if err := os.WriteFile(filepath.Join(tmp, "website"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	yml := []byte(`identifier: test-id
contexts:
  default: {}
stacks:
  default/website:
    root: website
    files: []
`)
	if err := os.WriteFile(filepath.Join(tmp, "dockform.yml"), yml, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	factory := dockercli.NewClientFactory()
	if err := Validate(context.Background(), cfg, factory); err == nil {
		t.Fatalf("expected app root not directory error")
	}
}

func TestValidate_Fails_WhenAgeKeyFileEmptyWithSopsSecrets(t *testing.T) {
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
	mustWrite(filepath.Join(tmp, "secrets.env"), "KEY=value\n")

	// Simulate empty AGE_KEY_FILE environment variable (which causes key_file to be empty after interpolation)
	yml := []byte(`identifier: test-id
contexts:
  default: {}
sops:
  age:
    key_file: ${AGE_KEY_FILE}
stacks:
  default/website:
    root: website
    files:
      - docker-compose.yaml
    secrets:
      sops:
        - secrets.env
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))

	// Ensure AGE_KEY_FILE is not set
	oldVal := os.Getenv("AGE_KEY_FILE")
	_ = os.Unsetenv("AGE_KEY_FILE")
	t.Cleanup(func() {
		if oldVal != "" {
			_ = os.Setenv("AGE_KEY_FILE", oldVal)
		}
	})

	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	factory := dockercli.NewClientFactory()
	err = Validate(context.Background(), cfg, factory)
	if err == nil {
		t.Fatalf("expected error when age key_file is empty but sops secrets are configured")
	}
	if !strings.Contains(err.Error(), "key_file is empty") {
		t.Errorf("expected error message to mention empty key_file, got: %v", err)
	}
	if !strings.Contains(err.Error(), "environment variable") {
		t.Errorf("expected error message to mention environment variable, got: %v", err)
	}
}

func TestValidate_Succeeds_WhenAgeKeyFileEmptyWithoutSopsSecrets(t *testing.T) {
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

	// Empty key_file but NO sops secrets - should pass
	yml := []byte(`identifier: test-id
contexts:
  default: {}
sops:
  age:
    key_file: ${AGE_KEY_FILE}
stacks:
  default/website:
    root: website
    files:
      - docker-compose.yaml
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))

	// Ensure AGE_KEY_FILE is not set
	_ = os.Unsetenv("AGE_KEY_FILE")

	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	factory := dockercli.NewClientFactory()
	err = Validate(context.Background(), cfg, factory)
	if err != nil {
		t.Fatalf("expected validation to succeed when no sops secrets configured, got: %v", err)
	}
}

func TestValidate_Succeeds_WhenNoSopsConfigured(t *testing.T) {
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
	mustWrite(filepath.Join(tmp, "website", "secrets.env"), "KEY=value\n")

	// SOPS secrets but no sops config section at all - should pass (treated as plaintext)
	yml := []byte(`identifier: test-id
contexts:
  default: {}
stacks:
  default/website:
    root: website
    files:
      - docker-compose.yaml
    secrets:
      sops:
        - secrets.env
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))

	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	factory := dockercli.NewClientFactory()
	err = Validate(context.Background(), cfg, factory)
	if err != nil {
		t.Fatalf("expected validation to succeed when no sops config section, got: %v", err)
	}
}

func TestValidate_MultipleDaemons(t *testing.T) {
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

	// Create compose files for multiple stacks across daemons
	mustWrite(filepath.Join(tmp, "web", "docker-compose.yaml"), "version: '3'\nservices: {}\n")
	mustWrite(filepath.Join(tmp, "api", "docker-compose.yaml"), "version: '3'\nservices: {}\n")

	yml := []byte(`identifier: my-project
contexts:
  local:
    volumes: {}
    networks: {}
  remote:
    volumes: {}
    networks: {}
stacks:
  local/web:
    root: web
    files:
      - docker-compose.yaml
  remote/api:
    root: api
    files:
      - docker-compose.yaml
`)
	mustWrite(filepath.Join(tmp, "dockform.yml"), string(yml))

	cfg, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	factory := dockercli.NewClientFactory()
	if err := Validate(context.Background(), cfg, factory); err != nil {
		t.Fatalf("validate: %v", err)
	}

	// Verify context configs were loaded correctly
	if len(cfg.Contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %d", len(cfg.Contexts))
	}
	if _, ok := cfg.Contexts["local"]; !ok {
		t.Errorf("local context not found")
	}
	if _, ok := cfg.Contexts["remote"]; !ok {
		t.Errorf("remote context not found")
	}
	if cfg.Identifier != "my-project" {
		t.Errorf("identifier mismatch: expected 'my-project', got '%s'", cfg.Identifier)
	}
}
