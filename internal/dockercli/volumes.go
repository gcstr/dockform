package dockercli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gcstr/dockform/internal/util"
)

// ListVolumes returns names of docker volumes.
func (c *Client) ListVolumes(ctx context.Context) ([]string, error) {
	args := []string{"volume", "ls", "--format", "{{.Name}}"}
	if c.identifier != "" {
		args = append(args, "--filter", "label=io.dockform.identifier="+c.identifier)
	}
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return util.SplitNonEmptyLines(out), nil
}

type VolumeCreateOpts struct {
	Driver  string
	Options map[string]string
}

// VolumeInspect represents subset of docker volume inspect we need
type VolumeInspect struct {
	Name    string            `json:"Name"`
	Driver  string            `json:"Driver"`
	Options map[string]string `json:"Options"`
	Labels  map[string]string `json:"Labels"`
}

func (c *Client) CreateVolume(ctx context.Context, name string, labels map[string]string, opts ...VolumeCreateOpts) error {
	args := []string{"volume", "create"}
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
	}
	args = append(args, name)
	_, err := c.exec.Run(ctx, args...)
	return err
}

func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	_, err := c.exec.Run(ctx, "volume", "rm", name)
	return err
}

// VolumeExists returns true if a volume with the given name exists in the Docker context,
// regardless of labels. This must not create the volume; it only lists existing names.
func (c *Client) VolumeExists(ctx context.Context, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	out, err := c.exec.Run(ctx, "volume", "ls", "--format", "{{.Name}}")
	if err != nil {
		return false, err
	}
	for _, v := range util.SplitNonEmptyLines(out) {
		if v == name {
			return true, nil
		}
	}
	return false, nil
}

// InspectVolume returns details about a docker volume
func (c *Client) InspectVolume(ctx context.Context, name string) (VolumeInspect, error) {
	if name == "" {
		return VolumeInspect{}, nil
	}
	out, err := c.exec.Run(ctx, "volume", "inspect", "-f", "{{json .}}", name)
	if err != nil {
		return VolumeInspect{}, err
	}
	var vi VolumeInspect
	if err := json.Unmarshal([]byte(out), &vi); err != nil {
		return VolumeInspect{}, err
	}
	return vi, nil
}

// CopyVolumeData copies all files from src volume to dst volume using a helper container.
func (c *Client) CopyVolumeData(ctx context.Context, src, dst string) error {
	if src == "" || dst == "" {
		return nil
	}
	// Use alpine helper to stream tar between mounts to preserve perms/owners as best-effort
	cmd := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:%s", src, "/.src"),
		"-v", fmt.Sprintf("%s:%s", dst, "/.dst"),
		HelperImage, "sh", "-c",
		"cd /.src && tar -cpf - . 2>/dev/null | (cd /.dst && tar -xpf -)",
	}
	_, err := c.exec.Run(ctx, cmd...)
	return err
}
