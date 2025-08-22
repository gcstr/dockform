package dockercli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%w: %s", err, truncate(stderr.String(), 512))
	}
	return stdout.String(), nil
}

func (s SystemExec) RunInDir(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	if s.ContextName != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_CONTEXT=%s", s.ContextName))
	}
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%w: %s", err, truncate(stderr.String(), 512))
	}
	return stdout.String(), nil
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

// ComposeUp runs docker compose up -d with the given parameters.
// workingDir is where compose files and relative paths are resolved.
func (c *Client) ComposeUp(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string) (string, error) {
	args := []string{"compose"}
	// Use merged labeled compose file when identifier is set; otherwise use user files
	if c.identifier != "" {
		if p, err := c.buildLabeledProjectTemp(ctx, workingDir, files, profiles, envFiles, projectName, c.identifier); err == nil && p != "" {
			defer os.Remove(p)
			args = append(args, "-f", filepath.Clean(p))
		} else {
			for _, f := range files {
				args = append(args, "-f", filepath.Clean(f))
			}
		}
	} else {
		for _, f := range files {
			args = append(args, "-f", filepath.Clean(f))
		}
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

// ComposeConfigDoc captures the relevant parts of docker compose config.
type ComposeConfigDoc struct {
	Services map[string]ComposeService `json:"services" yaml:"services"`
}

type ComposePort struct {
	Target    int         `json:"target" yaml:"target"`
	Published interface{} `json:"published" yaml:"published"`
	Protocol  string      `json:"protocol" yaml:"protocol"`
}

type ComposeService struct {
	Image string        `json:"image" yaml:"image"`
	Ports []ComposePort `json:"ports" yaml:"ports"`
}

// ComposeConfigFull renders the effective compose config and parses desired services info (image, etc.).
func (c *Client) ComposeConfigFull(ctx context.Context, workingDir string, files, profiles, envFiles []string) (ComposeConfigDoc, error) {
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
	// Prefer JSON when available
	argsJSON := append(append([]string{}, args...), "config", "--format", "json")
	out, err := c.exec.RunInDir(ctx, workingDir, argsJSON...)
	if err == nil {
		var doc ComposeConfigDoc
		if json.Unmarshal([]byte(out), &doc) == nil {
			return doc, nil
		}
	}
	// Fallback to YAML
	argsYAML := append(append([]string{}, args...), "config")
	out, err = c.exec.RunInDir(ctx, workingDir, argsYAML...)
	if err != nil {
		return ComposeConfigDoc{}, err
	}
	var doc ComposeConfigDoc
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		return ComposeConfigDoc{}, err
	}
	return doc, nil
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

// ComposeConfigHash returns the compose config hash for a single service.
// If identifier is non-empty, a temporary overlay compose file is used to add
// the label `dockform.identifier=<identifier>` to that service before hashing.
func (c *Client) ComposeConfigHash(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, service string, identifier string) (string, error) {
	args := []string{"compose"}
	// Use merged labeled compose file when identifier is set; otherwise use user files
	if identifier != "" {
		if p, err := c.buildLabeledProjectTemp(ctx, workingDir, files, profiles, envFiles, projectName, identifier); err == nil && p != "" {
			defer os.Remove(p)
			args = append(args, "-f", filepath.Clean(p))
		} else {
			for _, f := range files {
				args = append(args, "-f", filepath.Clean(f))
			}
		}
	} else {
		for _, f := range files {
			args = append(args, "-f", filepath.Clean(f))
		}
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
	args = append(args, "config", "--hash", service)
	out, err := c.exec.RunInDir(ctx, workingDir, args...)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(out)
	// Extract the last field from the first line: format is "<service> <hash>"
	firstLine := trimmed
	if idx := strings.IndexAny(trimmed, "\r\n"); idx >= 0 {
		firstLine = trimmed[:idx]
	}
	fields := strings.Fields(firstLine)
	if len(fields) == 0 {
		return "", fmt.Errorf("unexpected compose hash output: %s", truncate(trimmed, 200))
	}
	return fields[len(fields)-1], nil
}

// buildLabeledProjectTemp loads the effective compose yaml via `docker compose config`,
// injects dockform.identifier label into all services, writes to a temp file, and returns its path.
func (c *Client) buildLabeledProjectTemp(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, identifier string) (string, error) {
	if identifier == "" {
		return "", nil
	}
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
	args = append(args, "config")
	out, err := c.exec.RunInDir(ctx, workingDir, args...)
	if err != nil {
		return "", fmt.Errorf("compose config: %w", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		return "", fmt.Errorf("parse compose yaml: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	services, _ := doc["services"].(map[string]any)
	if services == nil {
		services = map[string]any{}
	}
	for name, val := range services {
		service, _ := val.(map[string]any)
		if service == nil {
			service = map[string]any{}
		}
		labels, _ := service["labels"].(map[string]any)
		if labels == nil {
			labels = map[string]any{}
		}
		labels["dockform.identifier"] = identifier
		service["labels"] = labels
		services[name] = service
	}
	doc["services"] = services
	b, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal labeled yaml: %w", err)
	}
	f, err := os.CreateTemp("", "dockform-labeled-project-*.yml")
	if err != nil {
		return "", fmt.Errorf("create temp project: %w", err)
	}
	path := f.Name()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write temp project: %w", err)
	}
	_ = f.Close()
	if os.Getenv("DOCKFORM_PRINT_OVERLAY") == "1" || os.Getenv("DOCKFORM_DEBUG_OVERLAY") == "1" {
		fmt.Fprintln(os.Stderr, "--- dockform labeled compose (merged) ---")
		fmt.Fprintf(os.Stderr, "path: %s\n", path)
		fmt.Fprintln(os.Stderr, string(b))
		fmt.Fprintln(os.Stderr, "--- end labeled ---")
	}
	return path, nil
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
