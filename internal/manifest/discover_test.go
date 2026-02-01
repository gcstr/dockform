package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverResources_ContextDirWithStacks(t *testing.T) {
	base := t.TempDir()

	// Create context directory with two stacks
	contextDir := filepath.Join(base, "prod")
	stackDir1 := filepath.Join(contextDir, "web")
	stackDir2 := filepath.Join(contextDir, "api")

	mustMkdir(t, stackDir1)
	mustMkdir(t, stackDir2)

	// Create compose files
	mustWriteFile(t, filepath.Join(stackDir1, "compose.yaml"), "services:\n  nginx: {}\n")
	mustWriteFile(t, filepath.Join(stackDir2, "docker-compose.yml"), "services:\n  api: {}\n")

	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"prod": {},
		},
	}

	if err := discoverResources(&cfg, base); err != nil {
		t.Fatalf("discoverResources: %v", err)
	}

	// Should discover both stacks
	if len(cfg.DiscoveredStacks) != 2 {
		t.Fatalf("expected 2 discovered stacks, got %d", len(cfg.DiscoveredStacks))
	}

	web, ok := cfg.DiscoveredStacks["prod/web"]
	if !ok {
		t.Fatal("expected discovered stack prod/web")
	}
	if web.Context != "prod" {
		t.Errorf("expected context 'prod', got %q", web.Context)
	}
	if len(web.Files) != 1 || filepath.Base(web.Files[0]) != "compose.yaml" {
		t.Errorf("expected compose.yaml, got %v", web.Files)
	}

	api, ok := cfg.DiscoveredStacks["prod/api"]
	if !ok {
		t.Fatal("expected discovered stack prod/api")
	}
	if len(api.Files) != 1 || filepath.Base(api.Files[0]) != "docker-compose.yml" {
		t.Errorf("expected docker-compose.yml, got %v", api.Files)
	}
}

func TestDiscoverResources_NoContextDir(t *testing.T) {
	base := t.TempDir()

	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"missing": {},
		},
	}

	// Should not error when context directory doesn't exist
	if err := discoverResources(&cfg, base); err != nil {
		t.Fatalf("discoverResources: %v", err)
	}

	if len(cfg.DiscoveredStacks) != 0 {
		t.Fatalf("expected 0 discovered stacks, got %d", len(cfg.DiscoveredStacks))
	}
}

func TestDiscoverResources_SkipDirsWithoutCompose(t *testing.T) {
	base := t.TempDir()

	contextDir := filepath.Join(base, "default")
	stackDir := filepath.Join(contextDir, "nocompose")
	mustMkdir(t, stackDir)

	// Create a directory without compose files
	mustWriteFile(t, filepath.Join(stackDir, "README.md"), "not a stack")

	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"default": {},
		},
	}

	if err := discoverResources(&cfg, base); err != nil {
		t.Fatalf("discoverResources: %v", err)
	}

	if len(cfg.DiscoveredStacks) != 0 {
		t.Fatalf("expected 0 discovered stacks, got %d", len(cfg.DiscoveredStacks))
	}
}

func TestDiscoverResources_SecretsDiscovery(t *testing.T) {
	base := t.TempDir()

	contextDir := filepath.Join(base, "prod")
	stackDir := filepath.Join(contextDir, "web")
	mustMkdir(t, stackDir)

	// Create compose file
	mustWriteFile(t, filepath.Join(stackDir, "compose.yaml"), "services:\n  web: {}\n")

	// Create context-level secrets
	mustWriteFile(t, filepath.Join(contextDir, "secrets.env"), "SECRET=val")

	// Create stack-level secrets
	mustWriteFile(t, filepath.Join(stackDir, "secrets.env"), "STACK_SECRET=val")

	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"prod": {},
		},
	}

	if err := discoverResources(&cfg, base); err != nil {
		t.Fatalf("discoverResources: %v", err)
	}

	stack := cfg.DiscoveredStacks["prod/web"]
	if len(stack.SopsSecrets) != 2 {
		t.Fatalf("expected 2 sops secrets (context + stack), got %d: %v", len(stack.SopsSecrets), stack.SopsSecrets)
	}
}

func TestDiscoverResources_EnvironmentFileDiscovery(t *testing.T) {
	base := t.TempDir()

	contextDir := filepath.Join(base, "default")
	stackDir := filepath.Join(contextDir, "app")
	mustMkdir(t, stackDir)

	mustWriteFile(t, filepath.Join(stackDir, "compose.yaml"), "services:\n  app: {}\n")
	mustWriteFile(t, filepath.Join(stackDir, "environment.env"), "FOO=bar")

	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"default": {},
		},
	}

	if err := discoverResources(&cfg, base); err != nil {
		t.Fatalf("discoverResources: %v", err)
	}

	stack := cfg.DiscoveredStacks["default/app"]
	if len(stack.EnvFile) != 1 || stack.EnvFile[0] != "environment.env" {
		t.Errorf("expected env-file [environment.env], got %v", stack.EnvFile)
	}
}

func TestDiscoverFilesets_VolumesDir(t *testing.T) {
	base := t.TempDir()

	contextDir := filepath.Join(base, "default")
	stackDir := filepath.Join(contextDir, "app")
	volumesDir := filepath.Join(stackDir, "volumes")
	configDir := filepath.Join(volumesDir, "config")
	dataDir := filepath.Join(volumesDir, "data")

	mustMkdir(t, configDir)
	mustMkdir(t, dataDir)

	// Create compose file
	mustWriteFile(t, filepath.Join(stackDir, "compose.yaml"), "services:\n  app: {}\n")

	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"default": {},
		},
	}

	if err := discoverResources(&cfg, base); err != nil {
		t.Fatalf("discoverResources: %v", err)
	}

	if len(cfg.DiscoveredFilesets) != 2 {
		t.Fatalf("expected 2 filesets, got %d", len(cfg.DiscoveredFilesets))
	}

	configFs, ok := cfg.DiscoveredFilesets["default/app/config"]
	if !ok {
		t.Fatal("expected fileset default/app/config")
	}
	if configFs.TargetVolume != "app_config" {
		t.Errorf("expected target_volume 'app_config', got %q", configFs.TargetVolume)
	}
	if configFs.TargetPath != "/" {
		t.Errorf("expected target_path '/', got %q", configFs.TargetPath)
	}
	if configFs.Context != "default" {
		t.Errorf("expected context 'default', got %q", configFs.Context)
	}
	if configFs.Stack != "app" {
		t.Errorf("expected stack 'app', got %q", configFs.Stack)
	}
	if !configFs.RestartServices.Attached {
		t.Error("expected restart_services to be attached")
	}

	dataFs, ok := cfg.DiscoveredFilesets["default/app/data"]
	if !ok {
		t.Fatal("expected fileset default/app/data")
	}
	if dataFs.TargetVolume != "app_data" {
		t.Errorf("expected target_volume 'app_data', got %q", dataFs.TargetVolume)
	}
}

func TestDiscoverFilesets_NoVolumesDir(t *testing.T) {
	base := t.TempDir()

	contextDir := filepath.Join(base, "default")
	stackDir := filepath.Join(contextDir, "app")
	mustMkdir(t, stackDir)

	mustWriteFile(t, filepath.Join(stackDir, "compose.yaml"), "services:\n  app: {}\n")

	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"default": {},
		},
	}

	if err := discoverResources(&cfg, base); err != nil {
		t.Fatalf("discoverResources: %v", err)
	}

	if len(cfg.DiscoveredFilesets) != 0 {
		t.Fatalf("expected 0 filesets, got %d", len(cfg.DiscoveredFilesets))
	}
}

func TestDiscoverResources_CustomConventions(t *testing.T) {
	base := t.TempDir()

	contextDir := filepath.Join(base, "default")
	stackDir := filepath.Join(contextDir, "app")
	mustMkdir(t, stackDir)

	// Use custom compose file name
	mustWriteFile(t, filepath.Join(stackDir, "stack.yml"), "services:\n  app: {}\n")
	mustWriteFile(t, filepath.Join(stackDir, "env.txt"), "FOO=bar")

	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"default": {},
		},
		Conventions: ConventionsConfig{
			ComposeFiles:    []string{"stack.yml"},
			EnvironmentFile: "env.txt",
			VolumesDir:      "data",
		},
	}

	if err := discoverResources(&cfg, base); err != nil {
		t.Fatalf("discoverResources: %v", err)
	}

	if len(cfg.DiscoveredStacks) != 1 {
		t.Fatalf("expected 1 discovered stack, got %d", len(cfg.DiscoveredStacks))
	}

	stack := cfg.DiscoveredStacks["default/app"]
	if filepath.Base(stack.Files[0]) != "stack.yml" {
		t.Errorf("expected stack.yml, got %v", stack.Files)
	}
	if len(stack.EnvFile) != 1 || stack.EnvFile[0] != "env.txt" {
		t.Errorf("expected env-file [env.txt], got %v", stack.EnvFile)
	}
}

func TestDiscoverResources_DisabledConventions(t *testing.T) {
	base := t.TempDir()

	contextDir := filepath.Join(base, "default")
	stackDir := filepath.Join(contextDir, "app")
	mustMkdir(t, stackDir)
	mustWriteFile(t, filepath.Join(stackDir, "compose.yaml"), "services:\n  app: {}\n")

	disabled := false
	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"default": {},
		},
		Conventions: ConventionsConfig{Enabled: &disabled},
	}

	// Conventions disabled - should not discover anything
	if cfg.Conventions.IsEnabled() {
		t.Fatal("expected conventions to be disabled")
	}
}

func TestFindComposeFile_Priority(t *testing.T) {
	dir := t.TempDir()

	// Create both compose.yaml and docker-compose.yml
	mustWriteFile(t, filepath.Join(dir, "compose.yaml"), "")
	mustWriteFile(t, filepath.Join(dir, "docker-compose.yml"), "")

	candidates := []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}
	result := findComposeFile(dir, candidates)

	if filepath.Base(result) != "compose.yaml" {
		t.Errorf("expected compose.yaml to take priority, got %q", result)
	}
}

func TestFindComposeFile_NotFound(t *testing.T) {
	dir := t.TempDir()

	candidates := []string{"compose.yaml", "compose.yml"}
	result := findComposeFile(dir, candidates)

	if result != "" {
		t.Errorf("expected empty string for no compose file, got %q", result)
	}
}

// helpers

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
