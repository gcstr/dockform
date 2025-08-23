package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	decrypt "github.com/getsops/sops/v3/decrypt"
	"github.com/goccy/go-yaml"
)

// DecryptAndParse returns key=value pairs from a SOPS-encrypted file.
// format: "dotenv", "yaml", or "json". Defaults to "dotenv" if empty.
func DecryptAndParse(ctx context.Context, path string, format string, ageKeyFile string) ([]string, error) {
	if format == "" {
		format = "dotenv"
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
		return nil, fmt.Errorf("sops decrypt %s: %w", path, err)
	}

	switch strings.ToLower(format) {
	case "dotenv":
		return parseDotenv(string(b)), nil
	case "yaml", "yml":
		return parseYAML(b)
	case "json":
		return parseJSON(b)
	default:
		// treat as dotenv-like
		return parseDotenv(string(b)), nil
	}
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

func parseYAML(b []byte) ([]string, error) {
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	return topLevelStringPairs(m), nil
}

func parseJSON(b []byte) ([]string, error) {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}
	return topLevelStringPairs(m), nil
}

func topLevelStringPairs(m map[string]any) []string {
	// Only collect top-level string values
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Deterministic output
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				pairs = append(pairs, fmt.Sprintf("%s=%s", k, s))
			}
		}
	}
	return pairs
}
