package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	decrypt "github.com/getsops/sops/v3/decrypt"
)

// DecryptAndParse returns key=value pairs from a SOPS-encrypted file.
// format: only "dotenv" is supported. Defaults to "dotenv" if empty.
func DecryptAndParse(ctx context.Context, path string, format string, ageKeyFile string) ([]string, error) {
	if format == "" {
		format = "dotenv"
	}
	if strings.ToLower(format) != "dotenv" {
		return nil, apperr.New("secrets.DecryptAndParse", apperr.InvalidInput, "unsupported secrets format %q: only \"dotenv\" is supported", format)
	}
	// Resolve home dir for key file if starts with ~/
	unset := false
	if ageKeyFile != "" {
		key := ageKeyFile
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
				defer os.Unsetenv("SOPS_AGE_KEY")
			} else {
				defer os.Setenv("SOPS_AGE_KEY", prevKey)
			}
		}
		if unset {
			defer os.Unsetenv("SOPS_AGE_KEY_FILE")
		} else {
			defer os.Setenv("SOPS_AGE_KEY_FILE", prev)
		}
	}

	// Decrypt file
	// The decrypt package uses env vars and does not need ctx.
	b, err := decrypt.File(path, format)
	if err != nil {
		return nil, apperr.Wrap("secrets.DecryptAndParse", apperr.External, err, "sops decrypt %s", path)
	}

	return parseDotenv(string(b)), nil
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
