package dockercli

import (
	"context"
	"fmt"

	"github.com/gcstr/dockform/internal/util"
)

// ListVolumes returns names of docker volumes.
func (c *Client) ListVolumes(ctx context.Context) ([]string, error) {
	args := []string{"volume", "ls", "--format", "{{.Name}}"}
	if c.identifier != "" {
		args = append(args, "--filter", "label=dockform.identifier="+c.identifier)
	}
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return util.SplitNonEmptyLines(out), nil
}

func (c *Client) CreateVolume(ctx context.Context, name string, labels map[string]string) error {
	args := []string{"volume", "create"}
	for k, v := range labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, name)
	_, err := c.exec.Run(ctx, args...)
	return err
}

func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	_, err := c.exec.Run(ctx, "volume", "rm", name)
	return err
}
