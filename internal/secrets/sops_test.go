package secrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDecryptAndParse_Dotenv_Success(t *testing.T) {
	requireSops(t)
	dir := t.TempDir()
	keyPath, recip := writeTempAgeKey(t, dir, true)
	// Isolate sops config from CI environment
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	_ = os.MkdirAll(filepath.Join(dir, ".config", "sops", "age"), 0o755)
	// Create plaintext and encrypt using our helper
	plainPath := filepath.Join(dir, "secret.env")
	if err := os.WriteFile(plainPath, []byte("FOO=bar\nBAZ='qux'\n"), 0o600); err != nil {
		t.Fatalf("write plaintext: %v", err)
	}
	if err := EncryptDotenvFileWithSops(context.Background(), plainPath, []string{recip}, keyPath); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Now decrypt via DecryptAndParse
	pairs, err := DecryptAndParse(context.Background(), plainPath, keyPath)
	if err != nil {
		t.Fatalf("DecryptAndParse: %v", err)
	}
	if len(pairs) != 2 || pairs[0] != "FOO=bar" || pairs[1] != "BAZ=qux" {
		t.Fatalf("unexpected pairs: %#v", pairs)
	}
}

func TestDecryptAndParse_MissingFile_Error(t *testing.T) {
	requireSops(t)
	dir := t.TempDir()
	keyPath, _ := writeTempAgeKey(t, dir, true)
	if _, err := DecryptAndParse(context.Background(), filepath.Join(dir, "missing.env"), keyPath); err == nil {
		t.Fatalf("expected decrypt error for missing file")
	}
}

func TestDecryptAndParse_PlaintextWhenEmptyKeyFile(t *testing.T) {
	// Test that when ageKeyFile is empty, the file is treated as plaintext
	dir := t.TempDir()
	plainPath := filepath.Join(dir, "plain.env")
	if err := os.WriteFile(plainPath, []byte("FOO=bar\nSECRET_KEY=from_sops\n"), 0o600); err != nil {
		t.Fatalf("write plaintext: %v", err)
	}
	// Call DecryptAndParse with empty ageKeyFile
	pairs, err := DecryptAndParse(context.Background(), plainPath, "")
	if err != nil {
		t.Fatalf("DecryptAndParse with empty key file: %v", err)
	}
	if len(pairs) != 2 || pairs[0] != "FOO=bar" || pairs[1] != "SECRET_KEY=from_sops" {
		t.Fatalf("unexpected pairs from plaintext: %#v", pairs)
	}
}

func TestParseDotenv_VariousCases(t *testing.T) {
	in := "\n# comment\nFOO=bar\n export BAR = \"baz\" \nBAZ='qux'\n=invalid\nONLYKEY\n\n"
	got := parseDotenv(in)
	// Expect: ignores comments/blank/invalid, trims export and quotes
	want := []string{"FOO=bar", "BAR=baz", "BAZ=qux"}
	if len(got) != len(want) {
		t.Fatalf("unexpected count: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mismatch at %d: got %q want %q", i, got[i], want[i])
		}
	}
}
