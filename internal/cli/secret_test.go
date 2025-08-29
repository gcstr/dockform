package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	age "filippo.io/age"
)

func writeTempAgeKey(t *testing.T, dir string) (keyPath string, recipient string) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate age identity: %v", err)
	}
	keyPath = filepath.Join(dir, "age.key")
	recipient = id.Recipient().String()
	content := id.String() + "\n# public key: " + recipient + "\n"
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

func TestSecret_Create_Success(t *testing.T) {
	requireSops(t)
	dir := t.TempDir()
	keyPath, _ := writeTempAgeKey(t, dir)
	// Isolate sops config from CI environment
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	_ = os.MkdirAll(filepath.Join(dir, ".config", "sops", "age"), 0o755)
	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)
	cfgPath := filepath.Join(dir, "dockform.yml")
	// Minimal config with required docker.identifier and sops key; recipients can be empty as they will be derived from key file
	cfg := "docker:\n  identifier: test-id\nsops:\n  age:\n    key_file: " + keyPath + "\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	target := filepath.Join(dir, "secrets.env")
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"secret", "create", target, "-c", cfgPath})
	if err := root.Execute(); err != nil {
		t.Fatalf("secret create execute: %v", err)
	}
	if st, err := os.Stat(target); err != nil || st.Size() == 0 {
		t.Fatalf("expected encrypted secrets file created; err=%v size=%d", err, func() int64 {
			if st != nil {
				return st.Size()
			}
			return 0
		}())
	}
	// Decrypt to verify contents
	cmd := exec.Command("sops", "--decrypt", "--input-type", "dotenv", target)
	cmd.Env = os.Environ()
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("decrypt created secret: %v", err)
	}
	if string(b) != "SECRET_KEY=secret\n" {
		t.Fatalf("unexpected plaintext after decrypt; got: %q", string(b))
	}
}

func TestSecret_Create_FileExists_Error(t *testing.T) {
	dir := t.TempDir()
	keyPath, _ := writeTempAgeKey(t, dir)
	cfgPath := filepath.Join(dir, "dockform.yml")
	cfg := "docker:\n  identifier: test-id\nsops:\n  age:\n    key_file: " + keyPath + "\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	target := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("precreate target: %v", err)
	}
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"secret", "create", target, "-c", cfgPath})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error when target exists, got nil")
	}
}

func TestSecret_Create_MissingKeyConfig_Error(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "dockform.yml")
	if err := os.WriteFile(cfgPath, []byte("docker:\n  identifier: test-id\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	target := filepath.Join(dir, "secrets.env")
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"secret", "create", target, "-c", cfgPath})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error for missing sops key config, got nil")
	}
}

func TestSecret_Rekey_Success(t *testing.T) {
	requireSops(t)
	dir := t.TempDir()
	keyPath, recipient := writeTempAgeKey(t, dir)
	// Isolate sops config from CI environment
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	_ = os.MkdirAll(filepath.Join(dir, ".config", "sops", "age"), 0o755)
	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)
	// First, create an encrypted secret using create
	cfgCreatePath := filepath.Join(dir, "create.yml")
	cfgCreate := "docker:\n  identifier: test-id\nsops:\n  age:\n    key_file: " + keyPath + "\n  recipients:\n    - " + recipient + "\n"
	if err := os.WriteFile(cfgCreatePath, []byte(cfgCreate), 0o644); err != nil {
		t.Fatalf("write create config: %v", err)
	}
	target := filepath.Join(dir, "secrets.env")
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"secret", "create", target, "-c", cfgCreatePath})
	if err := root.Execute(); err != nil {
		t.Fatalf("secret create execute: %v", err)
	}

	// Now, run rekey pointing to the created secret path via config
	cfgRekeyPath := filepath.Join(dir, "rekey.yml")
	cfgRekey := "docker:\n  identifier: test-id\nsops:\n  age:\n    key_file: " + keyPath + "\n  recipients:\n    - " + recipient + "\nsecrets:\n  sops:\n    - secrets.env\n"
	if err := os.WriteFile(cfgRekeyPath, []byte(cfgRekey), 0o644); err != nil {
		t.Fatalf("write rekey config: %v", err)
	}
	// Set cwd to dir so relative path in output is deterministic
	oldCwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldCwd) }()
	_ = os.Chdir(dir)

	out.Reset()
	root = newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"secret", "rekey", "-c", cfgRekeyPath})
	if err := root.Execute(); err != nil {
		t.Fatalf("secret rekey execute: %v", err)
	}
	if got := out.String(); got == "" || !bytes.Contains([]byte(got), []byte("secrets.env reencrypted\n")) {
		t.Fatalf("expected rekey output for secrets.env; got: %q", got)
	}
	// Ensure the file remains decryptable
	cmd := exec.Command("sops", "--decrypt", "--input-type", "dotenv", target)
	cmd.Env = os.Environ()
	if _, err := cmd.Output(); err != nil {
		t.Fatalf("decrypt after rekey: %v", err)
	}
}

func TestSecret_Rekey_DecryptError(t *testing.T) {
	dir := t.TempDir()
	keyPath, recipient := writeTempAgeKey(t, dir)
	cfgPath := filepath.Join(dir, "cfg.yml")
	cfg := "docker:\n  identifier: test-id\nsops:\n  age:\n    key_file: " + keyPath + "\n  recipients:\n    - " + recipient + "\nsecrets:\n  sops:\n    - missing.env\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"secret", "rekey", "-c", cfgPath})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected decrypt error for missing file, got nil")
	}
}
