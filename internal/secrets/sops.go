package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
)

// SopsOptions provides decryption/encryption settings for SOPS (age + pgp)
type SopsOptions struct {
	// Age
	AgeKeyFile    string
	AgeRecipients []string
	// PGP (GnuPG)
	PgpKeyringDir   string
	PgpUseAgent     bool
	PgpPinentryMode string // "default" | "loopback"
	PgpPassphrase   string // interpolated already; not logged
	PgpRecipients   []string
}

// buildSopsEnv builds environment variables for SOPS without mutating global process environment.
// This is safe for concurrent use across multiple goroutines.
func buildSopsEnv(opts SopsOptions) []string {
	// Start with a copy of current environment
	env := os.Environ()

	// Helper to set/replace an env var in the slice
	setEnv := func(key, value string) {
		prefix := key + "="
		for i, e := range env {
			if strings.HasPrefix(e, prefix) {
				env[i] = prefix + value
				return
			}
		}
		env = append(env, prefix+value)
	}

	// Prepare environment for SOPS/AGE
	if strings.TrimSpace(opts.AgeKeyFile) != "" {
		key := opts.AgeKeyFile
		if strings.HasPrefix(key, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				key = filepath.Join(home, key[2:])
			}
		}
		setEnv("SOPS_AGE_KEY_FILE", key)

		// Also set SOPS_AGE_KEY for environments where sops reads the key from env
		if b, err := os.ReadFile(key); err == nil {
			setEnv("SOPS_AGE_KEY", string(b))
		}
	}

	// Prepare environment for SOPS/PGP (GnuPG)
	if strings.TrimSpace(opts.PgpKeyringDir) != "" {
		dir := opts.PgpKeyringDir
		if strings.HasPrefix(dir, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				dir = filepath.Join(home, dir[2:])
			}
		}
		setEnv("GNUPGHOME", dir)

		// Loopback handling: request loopback mode if configured
		if strings.ToLower(strings.TrimSpace(opts.PgpPinentryMode)) == "loopback" && !opts.PgpUseAgent {
			setEnv("SOPS_GPG_EXEC", "gpg --pinentry-mode loopback")
		}
	}

	return env
}

// DecryptAndParse returns key=value pairs from a SOPS-encrypted dotenv file.
// Only dotenv format is supported. If no SOPS backends are configured, the file is treated as plaintext.
// This function is safe for concurrent use across multiple goroutines.
func DecryptAndParse(ctx context.Context, path string, opts SopsOptions) ([]string, error) {
	// If neither Age nor PGP is configured, treat as plaintext
	if strings.TrimSpace(opts.AgeKeyFile) == "" && strings.TrimSpace(opts.PgpKeyringDir) == "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, apperr.Wrap("secrets.DecryptAndParse", apperr.NotFound, err, "read plaintext file %s", path)
		}
		return parseDotenv(string(b)), nil
	}

	// Ensure sops binary exists
	if _, err := exec.LookPath("sops"); err != nil {
		return nil, apperr.New("secrets.DecryptAndParse", apperr.NotFound, "sops binary not found on PATH; please install sops")
	}

	// Build environment for subprocess without mutating global state
	env := buildSopsEnv(opts)

	// Decrypt file using system sops
	cmd := exec.CommandContext(ctx, "sops", "--decrypt", "--input-type", "dotenv", path)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, apperr.Wrap("secrets.DecryptAndParse", apperr.External, errors.New(string(ee.Stderr)), "sops decrypt %s", path)
		}
		return nil, apperr.Wrap("secrets.DecryptAndParse", apperr.External, err, "sops decrypt %s", path)
	}

	return parseDotenv(string(out)), nil
}

func parseDotenv(s string) []string {
	var pairs []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		val = strings.Trim(val, `"`)
		val = strings.Trim(val, `'`)
		if key == "" {
			continue
		}
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, val))
	}
	return pairs
}
