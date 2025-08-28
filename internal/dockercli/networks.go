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
		args = append(args, "--filter", "label=io.dockform/"+c.identifier)
	}
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return util.SplitNonEmptyLines(out), nil
}

func (c *Client) CreateNetwork(ctx context.Context, name string, labels map[string]string) error {
	args := []string{"network", "create"}
	for k, v := range labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, name)
	_, err := c.exec.Run(ctx, args...)
	return err
}

func (c *Client) RemoveNetwork(ctx context.Context, name string) error {
	_, err := c.exec.Run(ctx, "network", "rm", name)
	return err
}
