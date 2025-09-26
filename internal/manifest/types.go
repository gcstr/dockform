package manifest

import (
	"regexp"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
)

// Config is the root desired-state structure parsed from YAML.
type Config struct {
	Docker       DockerConfig                    `yaml:"docker"`
	Sops         *SopsConfig                     `yaml:"sops"`
	Secrets      *Secrets                        `yaml:"secrets"`
	Environment  *Environment                    `yaml:"environment"`
	Applications map[string]Application          `yaml:"applications" validate:"dive"`
	Networks     map[string]NetworkSpec          `yaml:"networks"`
	Volumes      map[string]TopLevelResourceSpec `yaml:"volumes"`
	Filesets     map[string]FilesetSpec          `yaml:"filesets"`
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
	SopsSecrets []string     `yaml:"-"`
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
	Sops []string `yaml:"sops"`
}

// TopLevelResourceSpec mirrors YAML for volumes.
type TopLevelResourceSpec struct{}

// NetworkSpec allows configuring docker network driver and options.
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

// FilesetSpec defines a local directory to sync into a docker volume at a target path.
type FilesetSpec struct {
	Source          string         `yaml:"source"`
	TargetVolume    string         `yaml:"target_volume"`
	TargetPath      string         `yaml:"target_path"`
	RestartServices RestartTargets `yaml:"restart_services"`
	ApplyMode       string         `yaml:"apply_mode"`
	Exclude         []string       `yaml:"exclude"`
	SourceAbs       string         `yaml:"-"`
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
	appKeyRegex = regexp.MustCompile(`^[a-z0-9_.-]+$`)
)
