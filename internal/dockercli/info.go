package dockercli

import (
	"context"
	"encoding/json"
	"strings"
)

// ServerVersion returns the Docker Engine server version for the configured context.
func (c *Client) ServerVersion(ctx context.Context) (string, error) {
	out, err := c.exec.Run(ctx, "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ContextHost returns the Docker host endpoint for the configured context.
// When a host override is set (from the manifest), it is returned directly.
// Falls back to the default context when none is set.
func (c *Client) ContextHost(ctx context.Context) (string, error) {
	if c.hostOverride != "" {
		return c.hostOverride, nil
	}
	name := c.contextName
	if strings.TrimSpace(name) == "" {
		name = "default"
	}
	out, err := c.exec.Run(ctx, "context", "inspect", name, "--format", "{{json .Endpoints.docker.Host}}")
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(out)
	// Trim optional surrounding quotes from Go template JSON output
	s = strings.TrimPrefix(s, "\"")
	s = strings.TrimSuffix(s, "\"")
	return s, nil
}

// ComposeVersion returns the docker compose plugin version (short form) if available.
func (c *Client) ComposeVersion(ctx context.Context) (string, error) {
	// Prefer short output when available
	out, err := c.exec.Run(ctx, "compose", "version", "--short")
	if err == nil {
		return strings.TrimSpace(out), nil
	}
	// Fallback to regular output (e.g., "Docker Compose version v2.29.7")
	out2, err2 := c.exec.Run(ctx, "compose", "version")
	if err2 != nil {
		return "", err2
	}
	return strings.TrimSpace(out2), nil
}

// ImageInspectRepoDigests returns the RepoDigests list for a local image.
// Returns nil and an error if the image is not found or inspect fails.
func (c *Client) ImageInspectRepoDigests(ctx context.Context, imageRef string) ([]string, error) {
	if strings.TrimSpace(imageRef) == "" {
		return nil, nil
	}
	out, err := c.exec.Run(ctx, "image", "inspect", "--format", "{{json .RepoDigests}}", imageRef)
	if err != nil {
		return nil, err
	}
	var digests []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &digests); err != nil {
		return nil, err
	}
	return digests, nil
}

// ImageExists returns true if the given image is present locally in the configured context.
func (c *Client) ImageExists(ctx context.Context, imageRef string) (bool, error) {
	if strings.TrimSpace(imageRef) == "" {
		return false, nil
	}
	_, err := c.exec.Run(ctx, "image", "inspect", imageRef)
	if err != nil {
		return false, nil
	}
	return true, nil
}
