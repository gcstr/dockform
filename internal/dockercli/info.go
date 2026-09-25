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

// RunningImage is the image a running compose container was created from.
type RunningImage struct {
	ID  string // full image ID (sha256:…), from compose's com.docker.compose.image label
	Ref string // the image as the container names it; an image ID instead once its tag moved to another image
}

// ComposeContainerImageMap returns a map of "project|service" → the image
// every running compose container was created from.
// A single docker ps call is used so cost is constant regardless of container
// count. docker ps has no image ID field, so the ID comes from the label
// compose sets on each container.
func (c *Client) ComposeContainerImageMap(ctx context.Context) (map[string]RunningImage, error) {
	out, err := c.exec.Run(ctx,
		"ps",
		"--no-trunc",
		"--filter", "label=com.docker.compose.service",
		"--format", `{{.Label "com.docker.compose.project"}}|{{.Label "com.docker.compose.service"}}|{{.Label "com.docker.compose.image"}}|{{.Image}}`,
	)
	if err != nil {
		return nil, err
	}
	result := make(map[string]RunningImage)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 || parts[0] == "" || parts[1] == "" {
			continue
		}
		result[parts[0]+"|"+parts[1]] = RunningImage{ID: parts[2], Ref: parts[3]}
	}
	return result, nil
}

// ImageRepoDigestMap returns a map of image ID → repo digest (sha256:…) for
// each of the given image IDs. A single docker image inspect call is issued
// for all IDs. Images not found locally are silently omitted.
func (c *Client) ImageRepoDigestMap(ctx context.Context, imageIDs []string) (map[string]string, error) {
	if len(imageIDs) == 0 {
		return make(map[string]string), nil
	}
	args := append([]string{"image", "inspect", "--format", `{{.Id}}|{{json .RepoDigests}}`}, imageIDs...)
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(imageIDs))
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sep := strings.Index(line, "|")
		if sep < 0 {
			continue
		}
		id := line[:sep]
		var digests []string
		if jsonErr := json.Unmarshal([]byte(line[sep+1:]), &digests); jsonErr != nil {
			continue
		}
		for _, rd := range digests {
			if idx := strings.LastIndex(rd, "@"); idx >= 0 {
				result[id] = rd[idx+1:]
				break
			}
		}
	}
	return result, nil
}

// LocalImageRefs lists every repository:tag stored on the daemon in a single
// docker image ls call. Untagged images are left out.
func (c *Client) LocalImageRefs(ctx context.Context) ([]string, error) {
	out, err := c.exec.Run(ctx, "image", "ls", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "<none>") {
			continue
		}
		refs = append(refs, line)
	}
	return refs, nil
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
