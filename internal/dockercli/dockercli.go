package dockercli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gcstr/dockform/internal/util"
)

// Client provides higher-level helpers around docker CLI.
type Client struct {
	exec        Exec
	identifier  string
	contextName string
}

func New(contextName string) *Client {
	return &Client{exec: SystemExec{ContextName: contextName}, contextName: contextName}
}

// WithIdentifier sets an optional label identifier to scope discovery.
func (c *Client) WithIdentifier(id string) *Client {
	c.identifier = id
	return c
}

// CheckDaemon verifies the docker daemon for the configured context is reachable.
func (c *Client) CheckDaemon(ctx context.Context) error {
	if _, err := c.exec.Run(ctx, "version", "--format", "{{.Server.Version}}"); err != nil {
		if c.contextName != "" {
			return fmt.Errorf("docker daemon not reachable (context=%s): %w", c.contextName, err)
		}
		return fmt.Errorf("docker daemon not reachable: %w", err)
	}
	return nil
}

// RemoveContainer removes a container by name. If force is true, the container
// will be stopped if running and removed.
func (c *Client) RemoveContainer(ctx context.Context, name string, force bool) error {
	args := []string{"container", "rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, name)
	_, err := c.exec.Run(ctx, args...)
	return err
}

// InspectContainerLabels returns selected labels from a container.
func (c *Client) InspectContainerLabels(ctx context.Context, containerName string, keys []string) (map[string]string, error) {
	if containerName == "" {
		return nil, fmt.Errorf("container name required")
	}
	out, err := c.exec.Run(ctx, "inspect", "-f", "{{json .Config.Labels}}", containerName)
	if err != nil {
		return nil, err
	}
	var labels map[string]string
	if err := json.Unmarshal([]byte(out), &labels); err != nil {
		return nil, fmt.Errorf("parse labels json: %w", err)
	}
	if len(keys) == 0 {
		return labels, nil
	}
	filtered := map[string]string{}
	for _, k := range keys {
		if v, ok := labels[k]; ok {
			filtered[k] = v
		}
	}
	return filtered, nil
}

// UpdateContainerLabels adds or updates labels for a running container.
func (c *Client) UpdateContainerLabels(ctx context.Context, containerName string, labels map[string]string) error {
	if len(labels) == 0 {
		return nil
	}
	args := []string{"container", "update"}
	for k, v := range labels {
		args = append(args, "--label-add", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, containerName)
	_, err := c.exec.Run(ctx, args...)
	return err
}

// ListComposeContainersAll lists all containers with compose labels (project/service) across the Docker context.
func (c *Client) ListComposeContainersAll(ctx context.Context) ([]PsBrief, error) {
	format := `{{.Label "com.docker.compose.project"}};{{.Label "com.docker.compose.service"}};{{.Names}}`
	args := []string{"ps", "-a", "--format", format}
	if c.identifier != "" {
		args = append(args, "--filter", "label=dockform.identifier="+c.identifier)
	}
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var items []PsBrief
	for _, line := range util.SplitNonEmptyLines(out) {
		parts := strings.SplitN(line, ";", 3)
		if len(parts) != 3 {
			continue
		}
		proj := strings.TrimSpace(parts[0])
		svc := strings.TrimSpace(parts[1])
		name := strings.TrimSpace(parts[2])
		if proj == "" || svc == "" {
			continue
		}
		items = append(items, PsBrief{Project: proj, Service: svc, Name: name})
	}
	return items, nil
}

// SyncDirToVolume streams a tar of localDir to the named volume's targetPath.
// Requirements:
// - targetPath must be absolute and not '/'
// - Ensure targetPath exists in the container (mkdir -p)
// - Mount the volume at targetPath and operate there
// - Remove current contents then extract tar stream into targetPath
func (c *Client) SyncDirToVolume(ctx context.Context, volumeName, targetPath, localDir string) error {
	if volumeName == "" {
		return fmt.Errorf("volume name required")
	}
	if !strings.HasPrefix(targetPath, "/") {
		return fmt.Errorf("targetPath must be absolute")
	}
	if targetPath == "/" {
		return fmt.Errorf("targetPath cannot be '/'")
	}
	cmd := []string{
		"run", "--rm", "-i",
		"-v", fmt.Sprintf("%s:%s", volumeName, targetPath),
		"alpine", "sh", "-c",
		"mkdir -p '" + targetPath + "' && rm -rf '" + targetPath + "'/* '" + targetPath + "'/.[!.]* '" + targetPath + "'/..?* 2>/dev/null || true; tar -xpf - -C '" + targetPath + "'",
	}
	pr, pw := io.Pipe()
	go func() {
		werr := util.TarDirectoryToWriter(localDir, "", pw)
		_ = pw.CloseWithError(werr)
	}()
	_, err := c.exec.RunWithStdin(ctx, pr, cmd...)
	return err
}
