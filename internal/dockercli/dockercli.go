package dockercli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Exec abstracts docker command execution for ease of testing.
type Exec interface {
	Run(ctx context.Context, args ...string) (string, error)
	RunInDir(ctx context.Context, dir string, args ...string) (string, error)
}

// SystemExec is a real implementation that shells out to the docker CLI.
type SystemExec struct {
	ContextName string
}

func (s SystemExec) Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	if s.ContextName != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_CONTEXT=%s", s.ContextName))
	}
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func (s SystemExec) RunInDir(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	if s.ContextName != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_CONTEXT=%s", s.ContextName))
	}
	if dir != "" {
		cmd.Dir = dir
	}
	b, err := cmd.CombinedOutput()
	return string(b), err
}

// Client provides higher-level helpers around docker CLI.
type Client struct {
	exec       Exec
	identifier string
}

func New(contextName string) *Client {
	return &Client{exec: SystemExec{ContextName: contextName}}
}

// WithIdentifier sets an optional label identifier to scope discovery.
func (c *Client) WithIdentifier(id string) *Client {
	c.identifier = id
	return c
}

// ComposeAPI defines the subset of compose-related operations used by planner (exported for fakes in tests).
type ComposeAPI interface {
	ListVolumes(ctx context.Context) ([]string, error)
	ListNetworks(ctx context.Context) ([]string, error)
	ComposeConfigServices(ctx context.Context, workingDir string, files, profiles, envFiles []string) ([]string, error)
	ComposePs(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string) ([]ComposePsItem, error)
}

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
	return splitNonEmptyLines(out), nil
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

// ListNetworks returns names of docker networks.
func (c *Client) ListNetworks(ctx context.Context) ([]string, error) {
	args := []string{"network", "ls", "--format", "{{.Name}}"}
	if c.identifier != "" {
		args = append(args, "--filter", "label=dockform.identifier="+c.identifier)
	}
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return splitNonEmptyLines(out), nil
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

// ComposeUp runs docker compose up -d with the given parameters.
// workingDir is where compose files and relative paths are resolved.
func (c *Client) ComposeUp(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string) (string, error) {
	args := []string{"compose"}
	for _, f := range files {
		args = append(args, "-f", filepath.Clean(f))
	}
	if projectName != "" {
		args = append(args, "-p", projectName)
	}
	for _, e := range envFiles {
		args = append(args, "--env-file", filepath.Clean(e))
	}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "up", "-d")
	return c.exec.RunInDir(ctx, workingDir, args...)
}

// ComposeConfigServices returns the list of service names that would be part of the project.
func (c *Client) ComposeConfigServices(ctx context.Context, workingDir string, files, profiles, envFiles []string) ([]string, error) {
	args := []string{"compose"}
	for _, f := range files {
		args = append(args, "-f", filepath.Clean(f))
	}
	for _, e := range envFiles {
		args = append(args, "--env-file", filepath.Clean(e))
	}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "config", "--services")
	out, err := c.exec.RunInDir(ctx, workingDir, args...)
	if err != nil {
		return nil, err
	}
	return splitNonEmptyLines(out), nil
}

// ComposePsItem is a subset of fields from `docker compose ps --format json`.
type ComposePsItem struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	Image   string `json:"Image"`
	State   string `json:"State"`
	Project string `json:"Project"`
}

// ComposePs lists running (or created) compose services for the project.
func (c *Client) ComposePs(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string) ([]ComposePsItem, error) {
	args := []string{"compose"}
	for _, f := range files {
		args = append(args, "-f", filepath.Clean(f))
	}
	if projectName != "" {
		args = append(args, "-p", projectName)
	}
	for _, e := range envFiles {
		args = append(args, "--env-file", filepath.Clean(e))
	}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	args = append(args, "ps", "--format", "json")
	out, err := c.exec.RunInDir(ctx, workingDir, args...)
	if err != nil {
		return nil, err
	}
	// Try array first
	var items []ComposePsItem
	if err := json.Unmarshal([]byte(out), &items); err == nil {
		return items, nil
	}
	// Try single object
	var single ComposePsItem
	if err := json.Unmarshal([]byte(out), &single); err == nil && single.Name != "" {
		return []ComposePsItem{single}, nil
	}
	// Try NDJSON (one object per line)
	var results []ComposePsItem
	dec := json.NewDecoder(strings.NewReader(out))
	for dec.More() {
		var it ComposePsItem
		if err := dec.Decode(&it); err != nil {
			break
		}
		results = append(results, it)
	}
	if len(results) > 0 {
		return results, nil
	}
	return nil, fmt.Errorf("unexpected compose ps json: %s", truncate(out, 256))
}

// PsBrief represents a container with compose labels.
type PsBrief struct {
	Project string
	Service string
	Name    string
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
	for _, line := range splitNonEmptyLines(out) {
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

func splitNonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
