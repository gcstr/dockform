package manifest

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A discovered stack (prod/web) with extra secrets.sops files, and an explicit
// stack (prod/legacy, root: outside the <context>/<stack> layout) whose only
// secrets come from secrets.sops.
func loadSecretsSopsFixture(t *testing.T, discovery string) Config {
	t.Helper()
	base := t.TempDir()
	web := filepath.Join(base, "prod", "web")
	legacy := filepath.Join(base, "apps", "legacy")
	mustMkdir(t, web)
	mustMkdir(t, legacy)
	mustWriteFile(t, filepath.Join(web, "compose.yaml"), "services:\n  web: {}\n")
	mustWriteFile(t, filepath.Join(web, "extra.env"), "EXTRA=1\n")
	mustWriteFile(t, filepath.Join(legacy, "compose.yaml"), "services:\n  app: {}\n")
	mustWriteFile(t, filepath.Join(legacy, "app.env"), "APP=1\n")
	mustWriteFile(t, filepath.Join(base, "dockform.yml"), `identifier: demo
`+discovery+`contexts:
  prod: {}
stacks:
  prod/web:
    secrets:
      sops: [extra.env]
  prod/legacy:
    root: apps/legacy
    files: [compose.yaml]
    secrets:
      sops: [app.env]
`)
	cfg, _, err := LoadWithWarnings(filepath.Join(base, "dockform.yml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg
}

func TestDeprecationWarnings_SecretsSopsOnDiscoveredStackOnly(t *testing.T) {
	cfg := loadSecretsSopsFixture(t, "")

	got := cfg.DeprecationWarnings()
	if len(got) != 1 {
		t.Fatalf("expected one warning (the discovered stack), got %q", got)
	}
	for _, want := range []string{"stack prod/web", "secrets.sops is deprecated for discovered stacks", "extra.env", "the stack's secrets.env"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("warning should mention %q, got: %s", want, got[0])
		}
	}

	// Deprecated, not removed: the extra file is still loaded.
	stacks := cfg.GetAllStacks()
	if !slices.ContainsFunc(stacks["prod/web"].SopsSecrets, func(p string) bool { return strings.HasSuffix(p, "extra.env") }) {
		t.Errorf("secrets.sops must keep working on discovered stacks, got %v", stacks["prod/web"].SopsSecrets)
	}
	// Explicit stacks keep secrets.sops as their way to load secrets.
	if !slices.ContainsFunc(stacks["prod/legacy"].SopsSecrets, func(p string) bool { return strings.HasSuffix(p, "app.env") }) {
		t.Errorf("explicit stack should load its secrets.sops file, got %v", stacks["prod/legacy"].SopsSecrets)
	}
}

func TestDeprecationWarnings_NamesTheConfiguredSecretsFile(t *testing.T) {
	cfg := loadSecretsSopsFixture(t, "discovery:\n  secrets_file: .secrets.env\n")
	got := cfg.DeprecationWarnings()
	if len(got) != 1 || !strings.Contains(got[0], "the stack's .secrets.env") {
		t.Fatalf("warning should name the configured secrets file, got %q", got)
	}
}
