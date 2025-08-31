package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/go-playground/validator/v10"
	"github.com/goccy/go-yaml"
)

var (
	validate = validator.New(validator.WithRequiredStructEnabled())
)

// envVarPattern matches ${VARNAME} placeholders for interpolation
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// LoadWithWarnings reads and validates configuration and returns missing env var names instead of printing.
func LoadWithWarnings(path string) (Config, []string, error) {
	guessed, err := resolveConfigPath(path)
	if err != nil {
		return Config{}, nil, err
	}

	// Ensure absolute path for consistent base directory resolution across environments
	guessedAbs, err := filepath.Abs(guessed)
	if err != nil {
		return Config{}, nil, apperr.Wrap("manifest.Load", apperr.InvalidInput, err, "abs path")
	}

	b, err := os.ReadFile(guessedAbs)
	if err != nil {
		return Config{}, nil, apperr.Wrap("manifest.Load", apperr.NotFound, err, "read config")
	}

	// Interpolate env placeholders before decoding YAML
	interpolated, missing := interpolateEnvPlaceholders(string(b))

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader([]byte(interpolated)), yaml.Validator(validate), yaml.Strict())
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, missing, apperr.New("manifest.Load", apperr.InvalidInput, "parse yaml: %s", yaml.FormatError(err, true, true))
	}

	baseDir := filepath.Dir(guessedAbs)
	cfg.BaseDir = baseDir
	if err := cfg.normalizeAndValidate(baseDir); err != nil {
		return Config{}, missing, err
	}
	return cfg, missing, nil
}

// Load reads and validates configuration from the provided path. When path is empty,
// it searches for dockform.yml or dockform.yaml in the current working directory.
func Load(path string) (Config, error) {
	cfg, missing, err := LoadWithWarnings(path)
	if err != nil {
		return Config{}, err
	}
	for _, name := range missing {
		fmt.Fprintf(os.Stderr, "warning: environment variable %s is not set; replacing with empty string\n", name)
	}
	return cfg, nil
}

// RenderWithWarnings reads the manifest file and returns interpolated YAML and the list of missing env var names.
func RenderWithWarnings(path string) (string, []string, error) {
	guessed, err := resolveConfigPath(path)
	if err != nil {
		return "", nil, err
	}

	guessedAbs, err := filepath.Abs(guessed)
	if err != nil {
		return "", nil, apperr.Wrap("manifest.Render", apperr.InvalidInput, err, "abs path")
	}

	b, err := os.ReadFile(guessedAbs)
	if err != nil {
		return "", nil, apperr.Wrap("manifest.Render", apperr.NotFound, err, "read config")
	}

	interpolated, missing := interpolateEnvPlaceholders(string(b))
	return interpolated, missing, nil
}

// Render reads the manifest file at the provided path (or discovers it like Load)
// and returns the YAML content with ${VAR} placeholders interpolated from the
// current environment. Missing variables are replaced with empty strings and a
// warning is emitted to stderr.
func Render(path string) (string, error) {
	interpolated, missing, err := RenderWithWarnings(path)
	if err != nil {
		return "", err
	}
	for _, name := range missing {
		fmt.Fprintf(os.Stderr, "warning: environment variable %s is not set; replacing with empty string\n", name)
	}
	return interpolated, nil
}

// interpolateEnvPlaceholders replaces ${VAR} occurrences with os.Getenv("VAR").
// It returns the interpolated string and a list of variable names that were missing.
func interpolateEnvPlaceholders(in string) (string, []string) {
	missingSet := map[string]struct{}{}
	out := envVarPattern.ReplaceAllStringFunc(in, func(m string) string {
		submatches := envVarPattern.FindStringSubmatch(m)
		if len(submatches) != 2 {
			return m
		}
		name := submatches[1]
		val, ok := os.LookupEnv(name)
		if !ok {
			missingSet[name] = struct{}{}
			return ""
		}
		return val
	})
	if len(missingSet) == 0 {
		return out, nil
	}
	miss := make([]string, 0, len(missingSet))
	for n := range missingSet {
		miss = append(miss, n)
	}
	// Keep a stable order for tests by sorting when multiple are missing
	if len(miss) > 1 {
		sort.Strings(miss)
	}
	return out, miss
}

func resolveConfigPath(path string) (string, error) {
	if path != "" {
		// If user provided a path, allow either a directory or a file.
		if info, err := os.Stat(path); err == nil {
			if info.IsDir() {
				for _, name := range []string{"dockform.yaml", "dockform.yml"} {
					candidate := filepath.Join(path, name)
					_, statErr := os.Stat(candidate)
					if statErr == nil {
						return candidate, nil
					}
					if !errors.Is(statErr, fs.ErrNotExist) {
						return "", apperr.Wrap("manifest.resolveConfigPath", apperr.Internal, statErr, "stat %s", candidate)
					}
				}
				return "", apperr.New("manifest.resolveConfigPath", apperr.NotFound, "no config file found in %s (looked for dockform.yaml or dockform.yml)", path)
			}
			// Path exists and is a file. Use it directly.
			return path, nil
		}
		// If path does not exist, treat it as a file path and let the read fail later.
		return path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", apperr.Wrap("manifest.resolveConfigPath", apperr.Internal, err, "getwd")
	}
	for _, name := range []string{"dockform.yaml", "dockform.yml"} {
		candidate := filepath.Join(cwd, name)
		_, statErr := os.Stat(candidate)
		if statErr == nil {
			return candidate, nil
		}
		if errors.Is(statErr, fs.ErrNotExist) {
			continue
		}
		return "", apperr.Wrap("manifest.resolveConfigPath", apperr.Internal, statErr, "stat %s", candidate)
	}
	return "", apperr.New("manifest.resolveConfigPath", apperr.NotFound, "no config file found (looked for dockform.yaml or dockform.yml)")
}
