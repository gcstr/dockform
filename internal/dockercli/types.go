package dockercli

type ComposeConfigDoc struct {
	Services map[string]ComposeService `json:"services" yaml:"services"`
}

type ComposePort struct {
	Target    int         `json:"target" yaml:"target"`
	Published interface{} `json:"published" yaml:"published"`
	Protocol  string      `json:"protocol" yaml:"protocol"`
}

type ComposeService struct {
	Image         string        `json:"image" yaml:"image"`
	ContainerName string        `json:"container_name" yaml:"container_name"`
	Ports         []ComposePort `json:"ports" yaml:"ports"`
}

// ComposePsItem is a subset of fields from `docker compose ps --format json`.
type ComposePsItem struct {
	Name       string             `json:"Name"`
	Service    string             `json:"Service"`
	Image      string             `json:"Image"`
	State      string             `json:"State"`
	Project    string             `json:"Project"`
	Publishers []ComposePublisher `json:"Publishers"`
}

type ComposePublisher struct {
	URL           string `json:"URL"`
	TargetPort    int    `json:"TargetPort"`
	PublishedPort int    `json:"PublishedPort"`
	Protocol      string `json:"Protocol"`
}

// PsBrief represents a container with compose labels.
type PsBrief struct {
	Project string
	Service string
	Name    string
}
