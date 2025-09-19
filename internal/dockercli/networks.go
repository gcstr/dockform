package dockercli

import (
	"context"
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
	Driver     string
	Options    map[string]string
	Internal   bool
	Attachable bool
	IPv6       bool
	Subnet     string
	Gateway    string
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
	}
	args = append(args, name)
	_, err := c.exec.Run(ctx, args...)
	return err
}

func (c *Client) RemoveNetwork(ctx context.Context, name string) error {
	_, err := c.exec.Run(ctx, "network", "rm", name)
	return err
}
