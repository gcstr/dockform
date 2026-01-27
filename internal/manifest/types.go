package manifest

import (
	"regexp"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
)

// Config is the root desired-state structure parsed from YAML.
// This is the new multi-daemon schema with convention-over-configuration support.
type Config struct {
	// Global settings
	Sops        *SopsConfig       `yaml:"sops"`
	Conventions ConventionsConfig `yaml:"conventions"`

	// Multi-daemon support
	Daemons     map[string]DaemonConfig     `yaml:"daemons"`
	Deployments map[string]DeploymentConfig `yaml:"deployments"`

	// Explicit overrides (optional - conventions discover most of this)
	// Stack keys are in "daemon/stack" format (e.g., "hetzner-one/traefik")
	Stacks map[string]Stack `yaml:"stacks" validate:"dive"`

	// Computed
	BaseDir string `yaml:"-"`

	// Discovered resources (populated by convention discovery)
	DiscoveredStacks   map[string]Stack       `yaml:"-"` // daemon/stack -> Stack
	DiscoveredFilesets map[string]FilesetSpec `yaml:"-"` // daemon/stack/fileset -> FilesetSpec
}

// DaemonConfig defines a Docker daemon/context to manage.
type DaemonConfig struct {
	Context    string `yaml:"context"`    // Docker context name
	Identifier string `yaml:"identifier"` // Resource label (io.dockform.identifier)
}

// DeploymentConfig defines a named deployment group for targeting multiple daemons/stacks.
type DeploymentConfig struct {
	Description string   `yaml:"description"`
	Daemons     []string `yaml:"daemons"` // Target all stacks in these daemons
	Stacks      []string `yaml:"stacks"`  // Target specific stacks (daemon/stack format)
}

// ConventionsConfig controls convention-over-configuration behavior.
type ConventionsConfig struct {
	Enabled         *bool    `yaml:"enabled"`          // Default: true
	ComposeFiles    []string `yaml:"compose_files"`    // Default: [compose.yaml, compose.yml, docker-compose.yaml, docker-compose.yml]
	SecretsFile     string   `yaml:"secrets_file"`     // Default: secrets.env
	EnvironmentFile string   `yaml:"environment_file"` // Default: environment.env
	VolumesDir      string   `yaml:"volumes_dir"`      // Default: volumes
}

// IsEnabled returns whether conventions are enabled (defaults to true).
func (c ConventionsConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// GetComposeFiles returns the compose file patterns to search for.
func (c ConventionsConfig) GetComposeFiles() []string {
	if len(c.ComposeFiles) == 0 {
		return []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}
	}
	return c.ComposeFiles
}

// GetSecretsFile returns the secrets file name to look for.
func (c ConventionsConfig) GetSecretsFile() string {
	if c.SecretsFile == "" {
		return "secrets.env"
	}
	return c.SecretsFile
}

// GetEnvironmentFile returns the environment file name to look for.
func (c ConventionsConfig) GetEnvironmentFile() string {
	if c.EnvironmentFile == "" {
		return "environment.env"
	}
	return c.EnvironmentFile
}

// GetVolumesDir returns the volumes directory name to look for.
func (c ConventionsConfig) GetVolumesDir() string {
	if c.VolumesDir == "" {
		return "volumes"
	}
	return c.VolumesDir
}

// Stack defines a Docker Compose stack to manage.
type Stack struct {
	Root        string       `yaml:"root"`      // Override: stack directory (relative to daemon dir)
	Files       []string     `yaml:"files"`     // Override: compose files
	Profiles    []string     `yaml:"profiles"`  // Compose profiles to activate
	EnvFile     []string     `yaml:"env-file"`  // Additional env files
	Environment *Environment `yaml:"environment"`
	Secrets     *Secrets     `yaml:"secrets"` // Additional SOPS secrets
	Project     *Project     `yaml:"project"` // Compose project name override

	// Computed fields
	Daemon      string   `yaml:"-"` // Which daemon this belongs to (from key prefix)
	EnvInline   []string `yaml:"-"` // Merged inline env vars
	SopsSecrets []string `yaml:"-"` // Merged SOPS secret paths
	RootAbs     string   `yaml:"-"` // Absolute path to stack root
}

// Project allows overriding the Compose project name.
type Project struct {
	Name string `yaml:"name"`
}

// Environment holds environment file references and inline variables.
type Environment struct {
	Files  []string `yaml:"files"`
	Inline []string `yaml:"inline"`
}

// SopsConfig configures SOPS provider(s) for secret decryption.
type SopsConfig struct {
	Age *SopsAgeConfig `yaml:"age"`
	// Recipients is deprecated; kept for migration error messaging
	Recipients []string       `yaml:"recipients"`
	Pgp        *SopsPgpConfig `yaml:"pgp"`
}

// SopsAgeConfig configures the Age backend for SOPS.
type SopsAgeConfig struct {
	KeyFile    string   `yaml:"key_file"`
	Recipients []string `yaml:"recipients"`
}

// SopsPgpConfig configures the PGP (GnuPG) backend for SOPS.
type SopsPgpConfig struct {
	KeyringDir   string   `yaml:"keyring_dir"`
	UseAgent     bool     `yaml:"use_agent"`
	PinentryMode string   `yaml:"pinentry_mode"`
	Recipients   []string `yaml:"recipients"`
	Passphrase   string   `yaml:"passphrase"`
}

// Secrets holds secret sources (SOPS-encrypted files).
type Secrets struct {
	Sops []string `yaml:"sops"`
}

// NetworkSpec allows configuring Docker network driver and options.
type NetworkSpec struct {
	Driver       string            `yaml:"driver"`
	Options      map[string]string `yaml:"options"`
	Internal     bool              `yaml:"internal"`
	Attachable   bool              `yaml:"attachable"`
	IPv6         bool              `yaml:"ipv6"`
	Subnet       string            `yaml:"subnet"`
	Gateway      string            `yaml:"gateway"`
	IPRange      string            `yaml:"ip_range"`
	AuxAddresses map[string]string `yaml:"aux_addresses"`
}

// Ownership defines optional ownership and permission settings for fileset files.
type Ownership struct {
	User             string `yaml:"user"`              // numeric UID string preferred; allow names
	Group            string `yaml:"group"`             // numeric GID string preferred; allow names
	FileMode         string `yaml:"file_mode"`         // octal string "0644" or "644"
	DirMode          string `yaml:"dir_mode"`          // octal string "0755" or "755"
	PreserveExisting bool   `yaml:"preserve_existing"` // if true, only apply to new/updated paths
}

// FilesetSpec defines a local directory to sync into a Docker volume at a target path.
type FilesetSpec struct {
	Source          string         `yaml:"source"`
	TargetVolume    string         `yaml:"target_volume"`
	TargetPath      string         `yaml:"target_path"`
	RestartServices RestartTargets `yaml:"restart_services"`
	ApplyMode       string         `yaml:"apply_mode"`
	Exclude         []string       `yaml:"exclude"`
	Ownership       *Ownership     `yaml:"ownership"`

	// Computed fields
	SourceAbs string `yaml:"-"`
	Daemon    string `yaml:"-"` // Which daemon this belongs to
	Stack     string `yaml:"-"` // Which stack this belongs to (for restart discovery)
}

// RestartTargets represents either an explicit list of services to restart, or
// the sentinel value "attached" which means: discover services that mount the
// fileset's target_volume.
type RestartTargets struct {
	Attached bool
	Services []string
}

// UnmarshalYAML supports either a string (must be "attached") or a list of strings.
// If omitted or null, it results in no restarts.
func (r *RestartTargets) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try string first
	var s string
	if err := unmarshal(&s); err == nil {
		s2 := strings.TrimSpace(strings.ToLower(s))
		if s2 == "" {
			*r = RestartTargets{}
			return nil
		}
		if s2 != "attached" {
			return apperr.New("manifest.RestartTargets.UnmarshalYAML", apperr.InvalidInput, "restart_services: string value must be 'attached' or a list")
		}
		*r = RestartTargets{Attached: true}
		return nil
	}
	// Try list of strings
	var list []string
	if err := unmarshal(&list); err == nil {
		// Normalize and dedupe while preserving order
		seen := map[string]struct{}{}
		out := make([]string, 0, len(list))
		for _, it := range list {
			v := strings.TrimSpace(it)
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
		*r = RestartTargets{Services: out}
		return nil
	}
	// Try interface{} to catch null
	var any interface{}
	if err := unmarshal(&any); err == nil {
		if any == nil {
			*r = RestartTargets{}
			return nil
		}
	}
	return apperr.New("manifest.RestartTargets.UnmarshalYAML", apperr.InvalidInput, "restart_services: must be 'attached' or list of service names")
}

var (
	appKeyRegex    = regexp.MustCompile(`^[a-z0-9_.-]+$`)
	daemonKeyRegex = regexp.MustCompile(`^[a-z0-9_-]+$`)
)

// ParseStackKey splits a "daemon/stack" key into its components.
func ParseStackKey(key string) (daemon, stack string, err error) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) != 2 {
		return "", "", apperr.New("manifest.ParseStackKey", apperr.InvalidInput, "stack key must be in 'daemon/stack' format: %s", key)
	}
	return parts[0], parts[1], nil
}

// MakeStackKey creates a "daemon/stack" key from components.
func MakeStackKey(daemon, stack string) string {
	return daemon + "/" + stack
}

// GetAllStacks returns all stacks (discovered + explicit overrides merged).
// Explicit stacks override discovered ones.
func (c *Config) GetAllStacks() map[string]Stack {
	result := make(map[string]Stack)

	// Start with discovered stacks
	for k, v := range c.DiscoveredStacks {
		result[k] = v
	}

	// Merge explicit stacks (override discovered)
	for k, v := range c.Stacks {
		if existing, ok := result[k]; ok {
			// Merge: explicit values override discovered
			merged := existing
			if v.Root != "" {
				merged.Root = v.Root
			}
			if len(v.Files) > 0 {
				merged.Files = v.Files
			}
			if len(v.Profiles) > 0 {
				merged.Profiles = v.Profiles
			}
			if len(v.EnvFile) > 0 {
				merged.EnvFile = v.EnvFile
			}
			if v.Environment != nil {
				merged.Environment = v.Environment
			}
			if v.Secrets != nil {
				merged.Secrets = v.Secrets
			}
			if v.Project != nil {
				merged.Project = v.Project
			}
			result[k] = merged
		} else {
			result[k] = v
		}
	}

	return result
}

// GetAllFilesets returns all filesets (discovered).
func (c *Config) GetAllFilesets() map[string]FilesetSpec {
	if c.DiscoveredFilesets == nil {
		return make(map[string]FilesetSpec)
	}
	return c.DiscoveredFilesets
}

// GetStacksForDaemon returns all stacks belonging to a specific daemon.
func (c *Config) GetStacksForDaemon(daemonName string) map[string]Stack {
	result := make(map[string]Stack)
	for key, stack := range c.GetAllStacks() {
		daemon, stackName, err := ParseStackKey(key)
		if err != nil {
			continue
		}
		if daemon == daemonName {
			result[stackName] = stack
		}
	}
	return result
}

// GetFilesetsForDaemon returns all filesets belonging to a specific daemon.
func (c *Config) GetFilesetsForDaemon(daemonName string) map[string]FilesetSpec {
	result := make(map[string]FilesetSpec)
	for key, fileset := range c.GetAllFilesets() {
		if fileset.Daemon == daemonName {
			result[key] = fileset
		}
	}
	return result
}

// GetAllSopsSecrets collects all unique SOPS secret file paths from all stacks.
func (c *Config) GetAllSopsSecrets() []string {
	seen := make(map[string]struct{})
	var result []string

	for _, stack := range c.GetAllStacks() {
		for _, sopsPath := range stack.SopsSecrets {
			if _, exists := seen[sopsPath]; !exists {
				seen[sopsPath] = struct{}{}
				result = append(result, sopsPath)
			}
		}
	}

	return result
}
