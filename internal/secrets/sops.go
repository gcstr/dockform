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

// DecryptAndParse returns key=value pairs from a SOPS-encrypted dotenv file.
// Only dotenv format is supported. If no SOPS backends are configured, the file is treated as plaintext.
func DecryptAndParse(ctx context.Context, path string, opts SopsOptions) ([]string, error) {
    // If neither Age nor PGP is configured, treat as plaintext
    if strings.TrimSpace(opts.AgeKeyFile) == "" && strings.TrimSpace(opts.PgpKeyringDir) == "" {
        b, err := os.ReadFile(path)
        if err != nil {
            return nil, apperr.Wrap("secrets.DecryptAndParse", apperr.NotFound, err, "read plaintext file %s", path)
        }
        return parseDotenv(string(b)), nil
    }

    // Prepare environment for SOPS/AGE
    // Resolve home dir for key file if starts with ~/
    if strings.TrimSpace(opts.AgeKeyFile) != "" {
        unset := false
        key := opts.AgeKeyFile
        if strings.HasPrefix(key, "~/") {
            if home, err := os.UserHomeDir(); err == nil {
                key = filepath.Join(home, key[2:])
            }
        }
        prev := os.Getenv("SOPS_AGE_KEY_FILE")
        if prev == "" {
            unset = true
        }
        _ = os.Setenv("SOPS_AGE_KEY_FILE", key)
        // Also set SOPS_AGE_KEY for environments where sops reads the key from env
        if b, rerr := os.ReadFile(key); rerr == nil {
            prevKey := os.Getenv("SOPS_AGE_KEY")
            _ = os.Setenv("SOPS_AGE_KEY", string(b))
            if prevKey == "" {
                defer func() { _ = os.Unsetenv("SOPS_AGE_KEY") }()
            } else {
                defer func() { _ = os.Setenv("SOPS_AGE_KEY", prevKey) }()
            }
        }
        if unset {
            defer func() { _ = os.Unsetenv("SOPS_AGE_KEY_FILE") }()
        } else {
            defer func() { _ = os.Setenv("SOPS_AGE_KEY_FILE", prev) }()
        }
    }

    // Prepare environment for SOPS/PGP (GnuPG)
    if strings.TrimSpace(opts.PgpKeyringDir) != "" {
        // Expand ~/
        dir := opts.PgpKeyringDir
        if strings.HasPrefix(dir, "~/") {
            if home, err := os.UserHomeDir(); err == nil {
                dir = filepath.Join(home, dir[2:])
            }
        }
        prev := os.Getenv("GNUPGHOME")
        _ = os.Setenv("GNUPGHOME", dir)
        defer func() {
            if prev == "" {
                _ = os.Unsetenv("GNUPGHOME")
            } else {
                _ = os.Setenv("GNUPGHOME", prev)
            }
        }()

        // Loopback handling: request loopback mode if configured
        if strings.ToLower(strings.TrimSpace(opts.PgpPinentryMode)) == "loopback" && !opts.PgpUseAgent {
            prevExec := os.Getenv("SOPS_GPG_EXEC")
            // append loopback flag; rely on gpg inheriting STDIN; passphrase may still be required by gpg
            _ = os.Setenv("SOPS_GPG_EXEC", "gpg --pinentry-mode loopback")
            defer func() {
                if prevExec == "" {
                    _ = os.Unsetenv("SOPS_GPG_EXEC")
                } else {
                    _ = os.Setenv("SOPS_GPG_EXEC", prevExec)
                }
            }()
        }
    }

    // Ensure sops binary exists
    if _, err := exec.LookPath("sops"); err != nil {
        return nil, apperr.New("secrets.DecryptAndParse", apperr.NotFound, "sops binary not found on PATH; please install sops")
    }

    // Decrypt file using system sops
    cmd := exec.CommandContext(ctx, "sops", "--decrypt", "--input-type", "dotenv", path)
    cmd.Env = os.Environ()
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
