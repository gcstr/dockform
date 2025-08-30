package manifest

import (
	"regexp"
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

// TopLevelResourceSpec mirrors YAML for volumes/networks.
type TopLevelResourceSpec struct{}

// FilesetSpec defines a local directory to sync into a docker volume at a target path.
type FilesetSpec struct {
	Source          string   `yaml:"source"`
	TargetVolume    string   `yaml:"target_volume"`
	TargetPath      string   `yaml:"target_path"`
	RestartServices []string `yaml:"restart_services"`
	Exclude         []string `yaml:"exclude"`
	SourceAbs       string   `yaml:"-"`
}

var (
	appKeyRegex = regexp.MustCompile(`^[a-z0-9_.-]+$`)
)
