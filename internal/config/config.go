package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-playground/validator/v10"
	"github.com/goccy/go-yaml"
)

// Config is the root desired-state structure parsed from YAML.
type Config struct {
	Docker       DockerConfig                    `yaml:"docker"`
	Environment  *Environment                    `yaml:"environment"`
	Applications map[string]Application          `yaml:"applications" validate:"dive"`
	Volumes      map[string]TopLevelResourceSpec `yaml:"volumes"`
	Networks     map[string]TopLevelResourceSpec `yaml:"networks"`
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
	Project     *Project     `yaml:"project"`
}

type Project struct {
	Name string `yaml:"name"`
}

// Environment holds environment file references
type Environment struct {
	Files []string `yaml:"files"`
}

// TopLevelResourceSpec mirrors YAML for volumes/networks.
type TopLevelResourceSpec struct{}

var (
	appKeyRegex = regexp.MustCompile(`^[a-z0-9_.-]+$`)
	validate    = validator.New(validator.WithRequiredStructEnabled())
)

// Load reads and validates configuration from the provided path. When path is empty,
// it searches for config.yml or config.yaml in the current working directory.
func Load(path string) (Config, error) {
	guessed, err := resolveConfigPath(path)
	if err != nil {
		return Config{}, err
	}

	b, err := os.ReadFile(guessed)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(b), yaml.Validator(validate), yaml.Strict())
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse yaml: %s", yaml.FormatError(err, true, true))
	}

	if err := cfg.normalizeAndValidate(filepath.Dir(guessed)); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func resolveConfigPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	for _, name := range []string{"config.yml", "config.yaml"} {
		candidate := filepath.Join(cwd, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		return "", fmt.Errorf("stat %s: %w", candidate, err)
	}
	return "", fmt.Errorf("no config file found (looked for config.yml or config.yaml)")
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

		if len(app.Files) == 0 {
			c.Applications[appName] = Application{
				Root:        resolvedRoot,
				Files:       []string{filepath.Join(resolvedRoot, "docker-compose.yml")},
				Profiles:    app.Profiles,
				EnvFile:     mergedEnv,
				Environment: app.Environment,
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
				Project:     app.Project,
			}
		}
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
