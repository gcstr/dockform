package secrets

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	age "filippo.io/age"
)

func writeTempAgeKey(t *testing.T, dir string, includeComment bool) (keyPath string, recipient string) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate age identity: %v", err)
	}
	recipient = id.Recipient().String()
	keyPath = filepath.Join(dir, "age.key")
	content := id.String() + "\n"
	if includeComment {
		content += "# public key: " + recipient + "\n"
	}
	if err := os.WriteFile(keyPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write age key: %v", err)
	}
	return
}

func requireSops(t *testing.T) {
	if _, err := exec.LookPath("sops"); err != nil {
		t.Skip("sops binary not found in PATH; skipping")
	}
}

func TestAgeRecipientsFromKeyFile_ReturnsRecipient(t *testing.T) {
	dir := t.TempDir()
	keyPath, recip := writeTempAgeKey(t, dir, true)
	recips, err := AgeRecipientsFromKeyFile(keyPath)
	if err != nil {
		t.Fatalf("AgeRecipientsFromKeyFile: %v", err)
	}
	found := false
	for _, r := range recips {
		if r == recip {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected recipient %q in %v", recip, recips)
	}
}

func TestEncryptDotenvFileWithSops_Success_AndDecryptable(t *testing.T) {
	requireSops(t)
	dir := t.TempDir()
	keyPath, recip := writeTempAgeKey(t, dir, true)
	// Isolate sops config from CI environment (cross-platform)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // Windows uses USERPROFILE
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	_ = os.MkdirAll(filepath.Join(dir, ".config", "sops", "age"), 0o755)
	// plaintext dotenv
	path := filepath.Join(dir, "secrets.env")
	plain := "FOO=bar\nHELLO=world\n"
	if err := os.WriteFile(path, []byte(plain), 0o600); err != nil {
		t.Fatalf("write plaintext: %v", err)
	}
	if err := EncryptDotenvFileWithSops(context.Background(), path, []string{recip}, keyPath, nil, "", false, "", ""); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// file should not contain plaintext
	enc, _ := os.ReadFile(path)
	if strings.Contains(string(enc), "FOO=bar") {
		t.Fatalf("ciphertext still contains plaintext: %q", string(enc))
	}
	// decrypt using system sops, requiring the key file
	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)
	cmd := exec.Command("sops", "--decrypt", "--input-type", "dotenv", path)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(out) != plain {
		t.Fatalf("decrypted mismatch: got %q want %q", string(out), plain)
	}
}

func TestEncryptDotenvFileWithSops_NoRecipients_Error(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.env")
	_ = os.WriteFile(path, []byte("K=V\n"), 0o600)
	if err := EncryptDotenvFileWithSops(context.Background(), path, nil, "", nil, "", false, "", ""); err == nil {
		t.Fatalf("expected error for no recipients, got nil")
	}
}

func TestEncryptDotenvFileWithSops_BadRecipient_Error(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.env")
	_ = os.WriteFile(path, []byte("K=V\n"), 0o600)
	if err := EncryptDotenvFileWithSops(context.Background(), path, []string{"not-a-valid-recipient"}, "", nil, "", false, "", ""); err == nil || !strings.Contains(err.Error(), "age recipient") {
		t.Fatalf("expected age recipient error, got %v", err)
	}
}
