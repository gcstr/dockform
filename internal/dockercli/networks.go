package dockercli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gcstr/dockform/internal/util"
)

// ListNetworks returns names of docker networks.
func (c *Client) ListNetworks(ctx context.Context) ([]string, error) {
	args := []string{"network", "ls", "--format", "{{.Name}}"}
	if c.identifier != "" {
		args = append(args, "--filter", "label=io.dockform.identifier="+c.identifier)
	}
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return util.SplitNonEmptyLines(out), nil
}

// NetworkCreateOpts represents supported docker network create flags.
type NetworkCreateOpts struct {
	Driver       string
	Options      map[string]string
	Internal     bool
	Attachable   bool
	IPv6         bool
	Subnet       string
	Gateway      string
	IPRange      string
	AuxAddresses map[string]string
}

func (c *Client) CreateNetwork(ctx context.Context, name string, labels map[string]string, opts ...NetworkCreateOpts) error {
	args := []string{"network", "create"}
	for k, v := range labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", k, v))
	}
	if len(opts) > 0 {
		o := opts[0]
		if o.Driver != "" {
			args = append(args, "--driver", o.Driver)
		}
		for k, v := range o.Options {
			args = append(args, "--opt", fmt.Sprintf("%s=%s", k, v))
		}
		if o.Internal {
			args = append(args, "--internal")
		}
		if o.Attachable {
			args = append(args, "--attachable")
		}
		if o.IPv6 {
			args = append(args, "--ipv6")
		}
		if o.Subnet != "" {
			args = append(args, "--subnet", o.Subnet)
		}
		if o.Gateway != "" {
			args = append(args, "--gateway", o.Gateway)
		}
		if o.IPRange != "" {
			args = append(args, "--ip-range", o.IPRange)
		}
		for k, v := range o.AuxAddresses {
			args = append(args, "--aux-address", fmt.Sprintf("%s=%s", k, v))
		}
	}
	args = append(args, name)
	_, err := c.exec.Run(ctx, args...)
	return err
}

func (c *Client) RemoveNetwork(ctx context.Context, name string) error {
	_, err := c.exec.Run(ctx, "network", "rm", name)
	return err
}

// NetworkInspectIPAMConfig represents a single IPAM config entry
type NetworkInspectIPAMConfig struct {
	Subnet       string            `json:"Subnet"`
	Gateway      string            `json:"Gateway"`
	IPRange      string            `json:"IPRange"`
	AuxAddresses map[string]string `json:"AuxiliaryAddresses"`
}

// NetworkInspectIPAM represents IPAM section of network inspect
type NetworkInspectIPAM struct {
	Driver string                     `json:"Driver"`
	Config []NetworkInspectIPAMConfig `json:"Config"`
}

// NetworkInspect represents the subset of docker network inspect we need
type NetworkInspect struct {
	Name       string             `json:"Name"`
	Driver     string             `json:"Driver"`
	Options    map[string]string  `json:"Options"`
	Internal   bool               `json:"Internal"`
	Attachable bool               `json:"Attachable"`
	EnableIPv6 bool               `json:"EnableIPv6"`
	IPAM       NetworkInspectIPAM `json:"IPAM"`
	Containers map[string]struct {
		Name string `json:"Name"`
	} `json:"Containers"`
}

// InspectNetwork returns details about a docker network
func (c *Client) InspectNetwork(ctx context.Context, name string) (NetworkInspect, error) {
	if name == "" {
		return NetworkInspect{}, nil
	}
	out, err := c.exec.Run(ctx, "network", "inspect", "-f", "{{json .}}", name)
	if err != nil {
		return NetworkInspect{}, err
	}
	var ni NetworkInspect
	if err := json.Unmarshal([]byte(out), &ni); err != nil {
		return NetworkInspect{}, err
	}
	return ni, nil
}
