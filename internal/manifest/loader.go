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

	// Run convention discovery (always enabled; use stacks: block to override)
	if err := discoverResources(&cfg, baseDir); err != nil {
		return Config{}, missing, err
	}

	// Normalize and validate the config
	if err := cfg.normalizeAndValidate(baseDir); err != nil {
		return Config{}, missing, err
	}

	return cfg, missing, nil
}

// Load reads and validates configuration from the provided path. When path is empty,
// it searches for manifest files in this order in the current working directory:
// dockform.yml, dockform.yaml, Dockform.yml, Dockform.yaml
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

// discoverResources runs convention-based discovery to find stacks and filesets.
func discoverResources(cfg *Config, baseDir string) error {
	if cfg.DiscoveredStacks == nil {
		cfg.DiscoveredStacks = make(map[string]Stack)
	}
	if cfg.DiscoveredFilesets == nil {
		cfg.DiscoveredFilesets = make(map[string]FilesetSpec)
	}

	// Discover stacks for each declared context
	for contextName := range cfg.Contexts {
		contextDir := filepath.Join(baseDir, contextName)

		// Check if context directory exists
		info, err := os.Stat(contextDir)
		if err != nil {
			if os.IsNotExist(err) {
				// Context directory doesn't exist - that's fine, just skip discovery
				continue
			}
			return apperr.Wrap("manifest.discoverResources", apperr.Internal, err, "stat context dir %s", contextDir)
		}
		if !info.IsDir() {
			// Not a directory - skip
			continue
		}

		// Find context-level secrets
		contextSecrets := findSecretsFile(contextDir, cfg.Discovery.GetSecretsFile())

		// List subdirectories as potential stacks
		entries, err := os.ReadDir(contextDir)
		if err != nil {
			return apperr.Wrap("manifest.discoverResources", apperr.Internal, err, "read context dir %s", contextDir)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			stackName := entry.Name()
			stackDir := filepath.Join(contextDir, stackName)

			// Look for compose file
			composeFile := findComposeFile(stackDir, cfg.Discovery.GetComposeFiles())
			if composeFile == "" {
				// No compose file found, not a stack
				continue
			}

			// Found a stack! Create the discovered stack entry
			stackKey := MakeStackKey(contextName, stackName)

			stack := Stack{
				Root:    stackDir,
				Files:   []string{composeFile},
				Context: contextName,
			}

			// Find stack-level secrets
			stackSecrets := findSecretsFile(stackDir, cfg.Discovery.GetSecretsFile())

			// Merge secrets: context-level first, then stack-level (stack wins)
			var sopsSecrets []string
			if contextSecrets != "" {
				// Rebase context secrets relative to stack dir
				if rel, err := filepath.Rel(stackDir, contextSecrets); err == nil {
					sopsSecrets = append(sopsSecrets, rel)
				} else {
					sopsSecrets = append(sopsSecrets, contextSecrets)
				}
			}
			if stackSecrets != "" {
				// Stack secrets are already relative to stack dir
				sopsSecrets = append(sopsSecrets, filepath.Base(stackSecrets))
			}
			if len(sopsSecrets) > 0 {
				stack.SopsSecrets = sopsSecrets
			}

			// Find environment file
			envFile := findEnvFile(stackDir, cfg.Discovery.GetEnvironmentFile())
			if envFile != "" {
				stack.EnvFile = []string{filepath.Base(envFile)}
			}

			cfg.DiscoveredStacks[stackKey] = stack

			// Discover filesets from volumes/ directory
			if err := discoverFilesets(cfg, contextName, stackName, stackDir); err != nil {
				return err
			}
		}
	}

	return nil
}

// discoverFilesets discovers filesets from the volumes/ directory of a stack.
func discoverFilesets(cfg *Config, contextName, stackName, stackDir string) error {
	volumesDir := filepath.Join(stackDir, cfg.Discovery.GetVolumesDir())

	info, err := os.Stat(volumesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No volumes directory, that's fine
		}
		return apperr.Wrap("manifest.discoverFilesets", apperr.Internal, err, "stat volumes dir %s", volumesDir)
	}
	if !info.IsDir() {
		return nil
	}

	entries, err := os.ReadDir(volumesDir)
	if err != nil {
		return apperr.Wrap("manifest.discoverFilesets", apperr.Internal, err, "read volumes dir %s", volumesDir)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		volumeName := entry.Name()
		sourceDir := filepath.Join(volumesDir, volumeName)

		// Convention: target volume is <stack>_<volumeName>
		targetVolume := stackName + "_" + volumeName

		// Fileset key: context/stack/volumeName
		filesetKey := fmt.Sprintf("%s/%s/%s", contextName, stackName, volumeName)

		fileset := FilesetSpec{
			Source:          sourceDir,
			SourceAbs:       sourceDir,
			TargetVolume:    targetVolume,
			TargetPath:      "/", // Default to root of volume (normalized to /data during mount)
			RestartServices: RestartTargets{Attached: true},
			ApplyMode:       "hot",
			Context:         contextName,
			Stack:           stackName,
		}

		cfg.DiscoveredFilesets[filesetKey] = fileset
	}

	return nil
}

// findComposeFile looks for a compose file in the given directory.
func findComposeFile(dir string, candidates []string) string {
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// findSecretsFile looks for a secrets file in the given directory.
func findSecretsFile(dir, filename string) string {
	path := filepath.Join(dir, filename)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

// findEnvFile looks for an environment file in the given directory.
func findEnvFile(dir, filename string) string {
	path := filepath.Join(dir, filename)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
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

// RenderWithWarningsAndPath reads the manifest file and returns interpolated YAML,
// the resolved file path, and the list of missing env var names.
func RenderWithWarningsAndPath(path string) (string, string, []string, error) {
	guessed, err := resolveConfigPath(path)
	if err != nil {
		return "", "", nil, err
	}

	guessedAbs, err := filepath.Abs(guessed)
	if err != nil {
		return "", "", nil, apperr.Wrap("manifest.Render", apperr.InvalidInput, err, "abs path")
	}

	b, err := os.ReadFile(guessedAbs)
	if err != nil {
		return "", "", nil, apperr.Wrap("manifest.Render", apperr.NotFound, err, "read config")
	}

	// Get relative path from current working directory
	cwd, err := os.Getwd()
	if err != nil {
		// Fallback to base filename if we can't get cwd
		interpolated, missing := interpolateEnvPlaceholders(string(b))
		return interpolated, filepath.Base(guessedAbs), missing, nil
	}

	relPath, err := filepath.Rel(cwd, guessedAbs)
	if err != nil {
		// Fallback to base filename if relative path calculation fails
		interpolated, missing := interpolateEnvPlaceholders(string(b))
		return interpolated, filepath.Base(guessedAbs), missing, nil
	}

	interpolated, missing := interpolateEnvPlaceholders(string(b))
	return interpolated, relPath, missing, nil
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
				for _, name := range []string{"dockform.yml", "dockform.yaml", "Dockform.yml", "Dockform.yaml"} {
					candidate := filepath.Join(path, name)
					_, statErr := os.Stat(candidate)
					if statErr == nil {
						return candidate, nil
					}
					if !errors.Is(statErr, fs.ErrNotExist) {
						return "", apperr.Wrap("manifest.resolveConfigPath", apperr.Internal, statErr, "stat %s", candidate)
					}
				}
				return "", apperr.New("manifest.resolveConfigPath", apperr.NotFound, "no config file found in %s (looked for dockform.yml, dockform.yaml, Dockform.yml, Dockform.yaml)", path)
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
	for _, name := range []string{"dockform.yml", "dockform.yaml", "Dockform.yml", "Dockform.yaml"} {
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
	return "", apperr.New("manifest.resolveConfigPath", apperr.NotFound, "no config file found (looked for dockform.yml, dockform.yaml, Dockform.yml, Dockform.yaml)")
}
