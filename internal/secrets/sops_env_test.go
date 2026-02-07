package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if len(entry) > len(prefix) && entry[:len(prefix)] == prefix {
			return entry[len(prefix):]
		}
	}
	return ""
}

func TestBuildSopsEnv_AgeFileSetsKeyFileAndInlineKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "age.key")
	if err := os.WriteFile(keyPath, []byte("AGE-SECRET-KEY-1TEST\n"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	env := buildSopsEnv(SopsOptions{AgeKeyFile: keyPath})
	if got := envValue(env, "SOPS_AGE_KEY_FILE"); got != keyPath {
		t.Fatalf("unexpected SOPS_AGE_KEY_FILE: %q", got)
	}
	if got := envValue(env, "SOPS_AGE_KEY"); got != "AGE-SECRET-KEY-1TEST\n" {
		t.Fatalf("unexpected SOPS_AGE_KEY: %q", got)
	}
}

func TestBuildSopsEnv_ExpandsHomeForAgeAndPgp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	ageDir := filepath.Join(home, ".config", "sops", "age")
	if err := os.MkdirAll(ageDir, 0o755); err != nil {
		t.Fatalf("mkdir age dir: %v", err)
	}
	ageFile := filepath.Join(ageDir, "keys.txt")
	if err := os.WriteFile(ageFile, []byte("AGE-SECRET-KEY-1TEST\n"), 0o600); err != nil {
		t.Fatalf("write age file: %v", err)
	}

	gnupgDir := filepath.Join(home, ".gnupg-test")
	if err := os.MkdirAll(gnupgDir, 0o700); err != nil {
		t.Fatalf("mkdir gnupg dir: %v", err)
	}

	env := buildSopsEnv(SopsOptions{
		AgeKeyFile:      "~/.config/sops/age/keys.txt",
		PgpKeyringDir:   "~/.gnupg-test",
		PgpPinentryMode: "loopback",
		PgpUseAgent:     false,
	})
	if got := envValue(env, "SOPS_AGE_KEY_FILE"); got != ageFile {
		t.Fatalf("unexpected expanded age path: %q", got)
	}
	if got := envValue(env, "GNUPGHOME"); got != gnupgDir {
		t.Fatalf("unexpected expanded gnupg path: %q", got)
	}
	if got := envValue(env, "SOPS_GPG_EXEC"); got != "gpg --pinentry-mode loopback" {
		t.Fatalf("expected loopback gpg exec, got: %q", got)
	}
}

func TestBuildSopsEnv_DoesNotSetLoopbackWhenAgentEnabled(t *testing.T) {
	env := buildSopsEnv(SopsOptions{
		PgpKeyringDir:   "/tmp/does-not-matter",
		PgpPinentryMode: "loopback",
		PgpUseAgent:     true,
	})
	if got := envValue(env, "SOPS_GPG_EXEC"); got != "" {
		t.Fatalf("did not expect SOPS_GPG_EXEC when pgp agent is enabled, got: %q", got)
	}
}
