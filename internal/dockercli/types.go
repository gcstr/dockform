package dockercli

import (
	"encoding/json"
	"fmt"
	"sort"
)

type ComposeConfigDoc struct {
	Services map[string]ComposeService `json:"services" yaml:"services"`
}

type ComposePort struct {
	Target    int         `json:"target" yaml:"target"`
	Published interface{} `json:"published" yaml:"published"`
	Protocol  string      `json:"protocol" yaml:"protocol"`
}

type ComposeService struct {
	Image         string                 `json:"image" yaml:"image"`
	ContainerName string                 `json:"container_name" yaml:"container_name"`
	Ports         []ComposePort          `json:"ports" yaml:"ports"`
	Networks      ComposeServiceNetworks `json:"networks" yaml:"networks"`
	Volumes       []ComposeServiceVolume `json:"volumes" yaml:"volumes"`
}

type ComposeServiceVolume struct {
	Type   string `json:"type" yaml:"type"`
	Source string `json:"source" yaml:"source"`
	Target string `json:"target" yaml:"target"`
}

type ComposeServiceNetworks []string

func (c *ComposeServiceNetworks) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || len(data) == 0 {
		*c = nil
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		sort.Strings(arr)
		*c = arr
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err == nil {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		*c = keys
		return nil
	}
	return fmt.Errorf("compose service networks: unexpected format: %s", string(data))
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
