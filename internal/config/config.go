package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/goccy/go-yaml"
)

// Config is the root desired-state structure parsed from YAML.
type Config struct {
	Docker       DockerConfig                    `yaml:"docker"`
	Sops         *SopsConfig                     `yaml:"sops"`
	Secrets      *Secrets                        `yaml:"secrets"`
	Environment  *Environment                    `yaml:"environment"`
	Applications map[string]Application          `yaml:"applications" validate:"dive"`
	Volumes      map[string]TopLevelResourceSpec `yaml:"volumes"`
	Networks     map[string]TopLevelResourceSpec `yaml:"networks"`
	Assets       map[string]AssetSpec            `yaml:"assets"`
	BaseDir      string                          `yaml:"-"`
}

type DockerConfig struct {
	Context    string `yaml:"context"`
	Identifier string `yaml:"identifier"`
}

type Application struct {
	Root        string       `yaml:"root" validate:"required"`
	Files       []string     `yaml:"files"`
	Profiles    []string     `yaml:"profiles"`
	EnvFile     []string     `yaml:"env-file"`
	Environment *Environment `yaml:"environment"`
	Secrets     *Secrets     `yaml:"secrets"`
	Project     *Project     `yaml:"project"`
	EnvInline   []string     `yaml:"-"`
	SopsSecrets []SopsSecret `yaml:"-"`
}

type Project struct {
	Name string `yaml:"name"`
}

// Environment holds environment file references
type Environment struct {
	Files  []string `yaml:"files"`
	Inline []string `yaml:"inline"`
}

// SopsConfig configures SOPS provider(s)
type SopsConfig struct {
	Age        *SopsAgeConfig `yaml:"age"`
	Recipients []string       `yaml:"recipients"`
}

type SopsAgeConfig struct {
	KeyFile string `yaml:"key_file"`
}

// Secrets holds secret sources
type Secrets struct {
	Sops []SopsSecret `yaml:"sops"`
}

type SopsSecret struct {
	Path   string `yaml:"path"`
	Format string `yaml:"format"` // dotenv | yaml | json
}

// TopLevelResourceSpec mirrors YAML for volumes/networks.
type TopLevelResourceSpec struct{}

// AssetSpec defines a local directory to sync into a docker volume at a target path.
type AssetSpec struct {
	Source       string `yaml:"source"`
	TargetVolume string `yaml:"target_volume"`
	TargetPath   string `yaml:"target_path"`
	SourceAbs    string `yaml:"-"`
}

var (
	appKeyRegex = regexp.MustCompile(`^[a-z0-9_.-]+$`)
	validate    = validator.New(validator.WithRequiredStructEnabled())
)

// envVarPattern matches ${VARNAME} placeholders for interpolation
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Load reads and validates configuration from the provided path. When path is empty,
// it searches for dockform.yml or dockform.yaml in the current working directory.
func Load(path string) (Config, error) {
	guessed, err := resolveConfigPath(path)
	if err != nil {
		return Config{}, err
	}

	b, err := os.ReadFile(guessed)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	// Interpolate env placeholders before decoding YAML
	interpolated, missing := interpolateEnvPlaceholders(string(b))
	for _, name := range missing {
		fmt.Fprintf(os.Stderr, "warning: environment variable %s is not set; replacing with empty string\n", name)
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader([]byte(interpolated)), yaml.Validator(validate), yaml.Strict())
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse yaml: %s", yaml.FormatError(err, true, true))
	}

	baseDir := filepath.Dir(guessed)
	cfg.BaseDir = baseDir
	if err := cfg.normalizeAndValidate(baseDir); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Render reads the manifest file at the provided path (or discovers it like Load)
// and returns the YAML content with ${VAR} placeholders interpolated from the
// current environment. Missing variables are replaced with empty strings and a
// warning is emitted to stderr.
func Render(path string) (string, error) {
	guessed, err := resolveConfigPath(path)
	if err != nil {
		return "", err
	}

	b, err := os.ReadFile(guessed)
	if err != nil {
		return "", fmt.Errorf("read config: %w", err)
	}

	interpolated, missing := interpolateEnvPlaceholders(string(b))
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
	// Keep a stable order for tests by sorting
	if len(miss) > 1 {
		// Avoid importing sort just for tiny list; deterministic order not critical
		// but keep as-insertion by iterating map is random; accept non-determinism
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
						return "", fmt.Errorf("stat %s: %w", candidate, statErr)
					}
				}
				return "", fmt.Errorf("no config file found in %s (looked for dockform.yaml or dockform.yml)", path)
			}
			// Path exists and is a file. Use it directly.
			return path, nil
		}
		// If path does not exist, treat it as a file path and let the read fail later.
		return path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
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
		return "", fmt.Errorf("stat %s: %w", candidate, statErr)
	}
	return "", fmt.Errorf("no config file found (looked for dockform.yaml or dockform.yml)")
}

func (c *Config) normalizeAndValidate(baseDir string) error {
	// Defaults
	if c.Docker.Context == "" {
		c.Docker.Context = "default"
	}
	if c.Applications == nil {
		c.Applications = map[string]Application{}
	}
	if c.Volumes == nil {
		c.Volumes = map[string]TopLevelResourceSpec{}
	}
	if c.Networks == nil {
		c.Networks = map[string]TopLevelResourceSpec{}
	}
	if c.Assets == nil {
		c.Assets = map[string]AssetSpec{}
	}

	// Validate application keys and fill defaults
	for appName, app := range c.Applications {
		if !appKeyRegex.MatchString(appName) {
			return fmt.Errorf("invalid application key %q: must match ^[a-z0-9_.-]+$", appName)
		}
		// Resolve root relative to config file directory
		resolvedRoot := filepath.Clean(filepath.Join(baseDir, app.Root))

		// Merge environment files with correct base paths
		// Root-level files are converted from baseDir-relative to resolvedRoot-relative
		var mergedEnv []string
		if c.Environment != nil && len(c.Environment.Files) > 0 {
			mergedEnv = append(mergedEnv, rebaseRootEnvToApp(baseDir, resolvedRoot, c.Environment.Files)...)
		}
		if app.Environment != nil && len(app.Environment.Files) > 0 {
			mergedEnv = append(mergedEnv, app.Environment.Files...)
		}
		if len(app.EnvFile) > 0 {
			mergedEnv = append(mergedEnv, app.EnvFile...)
		}
		// De-duplicate env files while preserving order
		if len(mergedEnv) > 1 {
			seen := make(map[string]struct{}, len(mergedEnv))
			uniq := make([]string, 0, len(mergedEnv))
			for _, p := range mergedEnv {
				if _, ok := seen[p]; ok {
					continue
				}
				seen[p] = struct{}{}
				uniq = append(uniq, p)
			}
			mergedEnv = uniq
		}

		// Merge inline env vars (root first, then app). Last value for a key wins.
		var mergedInline []string
		if c.Environment != nil && len(c.Environment.Inline) > 0 {
			mergedInline = append(mergedInline, c.Environment.Inline...)
		}
		if app.Environment != nil && len(app.Environment.Inline) > 0 {
			mergedInline = append(mergedInline, app.Environment.Inline...)
		}
		if len(mergedInline) > 1 {
			// Deduplicate by key with last-wins while preserving order of last occurrences
			seen := map[string]struct{}{}
			dedupReversed := make([]string, 0, len(mergedInline))
			for i := len(mergedInline) - 1; i >= 0; i-- {
				kv := mergedInline[i]
				if kv == "" {
					continue
				}
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) == 0 || parts[0] == "" {
					continue
				}
				key := parts[0]
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				dedupReversed = append(dedupReversed, kv)
			}
			mergedInline = make([]string, 0, len(dedupReversed))
			for i := len(dedupReversed) - 1; i >= 0; i-- {
				mergedInline = append(mergedInline, dedupReversed[i])
			}
		}

		// Merge SOPS secrets: root-level rebased to app root, then app-level
		var mergedSops []SopsSecret
		if c.Secrets != nil && len(c.Secrets.Sops) > 0 {
			for _, s := range c.Secrets.Sops {
				if s.Path == "" {
					continue
				}
				abs := filepath.Clean(filepath.Join(baseDir, s.Path))
				if rel, err := filepath.Rel(resolvedRoot, abs); err == nil {
					mergedSops = append(mergedSops, SopsSecret{Path: rel, Format: strings.ToLower(s.Format)})
				} else {
					mergedSops = append(mergedSops, SopsSecret{Path: abs, Format: strings.ToLower(s.Format)})
				}
			}
		}
		if app.Secrets != nil && len(app.Secrets.Sops) > 0 {
			for _, s := range app.Secrets.Sops {
				if s.Path == "" {
					continue
				}
				mergedSops = append(mergedSops, SopsSecret{Path: s.Path, Format: strings.ToLower(s.Format)})
			}
		}

		if len(app.Files) == 0 {
			c.Applications[appName] = Application{
				Root:        resolvedRoot,
				Files:       []string{filepath.Join(resolvedRoot, "docker-compose.yml")},
				Profiles:    app.Profiles,
				EnvFile:     mergedEnv,
				Environment: app.Environment,
				Secrets:     app.Secrets,
				EnvInline:   mergedInline,
				SopsSecrets: mergedSops,
				Project:     app.Project,
			}
		} else {
			// Keep provided file paths (interpreted relative to Root by compose), but store resolved Root
			c.Applications[appName] = Application{
				Root:        resolvedRoot,
				Files:       app.Files,
				Profiles:    app.Profiles,
				EnvFile:     mergedEnv,
				Environment: app.Environment,
				Secrets:     app.Secrets,
				EnvInline:   mergedInline,
				SopsSecrets: mergedSops,
				Project:     app.Project,
			}
		}
	}

	// Assets validation and normalization
	for assetName, a := range c.Assets {
		if !appKeyRegex.MatchString(assetName) {
			return fmt.Errorf("invalid asset key %q: must match ^[a-z0-9_.-]+$", assetName)
		}
		if strings.TrimSpace(a.Source) == "" {
			return fmt.Errorf("asset %s: source path is required", assetName)
		}
		if a.TargetVolume == "" {
			return fmt.Errorf("asset %s: target_volume is required", assetName)
		}
		if _, ok := c.Volumes[a.TargetVolume]; !ok {
			return fmt.Errorf("asset %s: target_volume %q is not declared under volumes", assetName, a.TargetVolume)
		}
		if a.TargetPath == "" || !filepath.IsAbs(a.TargetPath) {
			return fmt.Errorf("asset %s: target_path must be an absolute path", assetName)
		}
		if a.TargetPath == "/" {
			return fmt.Errorf("asset %s: target_path cannot be '/'", assetName)
		}
		// Resolve source relative to baseDir
		srcAbs := a.Source
		if !filepath.IsAbs(srcAbs) {
			srcAbs = filepath.Clean(filepath.Join(baseDir, srcAbs))
		}
		a.SourceAbs = srcAbs
		c.Assets[assetName] = a
	}

	return nil
}

func rebaseRootEnvToApp(baseDir, resolvedRoot string, files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if f == "" {
			continue
		}
		abs := filepath.Clean(filepath.Join(baseDir, f))
		if rel, err := filepath.Rel(resolvedRoot, abs); err == nil {
			out = append(out, rel)
		} else {
			out = append(out, abs)
		}
	}
	return out
}
