package dockercli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/util"
	"github.com/goccy/go-yaml"
)

// ComposeUp runs docker compose up -d with the given parameters.
// workingDir is where compose files and relative paths are resolved.
func (c *Client) ComposeUp(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, inlineEnv []string) (string, error) {
	// Choose compose files (overlay or user files)
	chosenFiles := files
	if c.identifier != "" {
		if pth, err := c.buildLabeledProjectTemp(ctx, workingDir, files, profiles, envFiles, projectName, c.identifier, inlineEnv); err == nil && pth != "" {
			defer func() { _ = os.Remove(pth) }()
			chosenFiles = []string{pth}
		}
	}
	args := c.composeBaseArgs(chosenFiles, profiles, envFiles, projectName)
	args = append(args, "up", "-d")

	// DEBUG: Print the command to be run
	// cmdStr := "docker " + strings.Join(args, " ")
	// if len(inlineEnv) > 0 {
	// 	fmt.Fprintf(os.Stderr, "RUN: %s %s\n", strings.Join(inlineEnv, " "), cmdStr)
	// } else {
	// 	fmt.Fprintf(os.Stderr, "RUN: %s\n", cmdStr)
	// }
	if len(inlineEnv) > 0 {
		return c.exec.RunInDirWithEnv(ctx, workingDir, inlineEnv, args...)
	}
	return c.exec.RunInDir(ctx, workingDir, args...)
}

// ComposeConfigServices returns the list of service names that would be part of the project.
func (c *Client) ComposeConfigServices(ctx context.Context, workingDir string, files, profiles, envFiles []string, inlineEnv []string) ([]string, error) {
	args := c.composeBaseArgs(files, profiles, envFiles, "")
	args = append(args, "config", "--services")
	var out string
	var err error
	if len(inlineEnv) > 0 {
		out, err = c.exec.RunInDirWithEnv(ctx, workingDir, inlineEnv, args...)
	} else {
		out, err = c.exec.RunInDir(ctx, workingDir, args...)
	}
	if err != nil {
		return nil, err
	}
	return util.SplitNonEmptyLines(out), nil
}

// ComposeConfigFull renders the effective compose config and parses desired services info (image, etc.).
func (c *Client) ComposeConfigFull(ctx context.Context, workingDir string, files, profiles, envFiles []string, inlineEnv []string) (ComposeConfigDoc, error) {
	args := c.composeBaseArgs(files, profiles, envFiles, "")
	// Prefer JSON when available
	argsJSON := append(append([]string{}, args...), "config", "--format", "json")
	var out string
	var err error
	if len(inlineEnv) > 0 {
		out, err = c.exec.RunInDirWithEnv(ctx, workingDir, inlineEnv, argsJSON...)
	} else {
		out, err = c.exec.RunInDir(ctx, workingDir, argsJSON...)
	}
	if err == nil {
		var doc ComposeConfigDoc
		if json.Unmarshal([]byte(out), &doc) == nil {
			return doc, nil
		}
	}
	// Fallback to YAML
	argsYAML := append(append([]string{}, args...), "config")
	if len(inlineEnv) > 0 {
		out, err = c.exec.RunInDirWithEnv(ctx, workingDir, inlineEnv, argsYAML...)
	} else {
		out, err = c.exec.RunInDir(ctx, workingDir, argsYAML...)
	}
	if err != nil {
		return ComposeConfigDoc{}, apperr.Wrap("dockercli.ComposeConfigFull", apperr.Internal, err, "parse compose yaml")
	}
	var doc ComposeConfigDoc
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		return ComposeConfigDoc{}, apperr.Wrap("dockercli.ComposeConfigFull", apperr.Internal, err, "parse compose yaml")
	}
	return doc, nil
}

// ComposePs lists running (or created) compose services for the project.
func (c *Client) ComposePs(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, inlineEnv []string) ([]ComposePsItem, error) {
	args := c.composeBaseArgs(files, profiles, envFiles, projectName)
	args = append(args, "ps", "--format", "json")
	var out string
	var err error
	if len(inlineEnv) > 0 {
		out, err = c.exec.RunInDirWithEnv(ctx, workingDir, inlineEnv, args...)
	} else {
		out, err = c.exec.RunInDir(ctx, workingDir, args...)
	}
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
	return nil, apperr.New("dockercli.ComposePs", apperr.External, "unexpected compose ps json: %s", util.Truncate(out, 256))
}

// ComposeConfigHash returns the compose config hash for a single service.
// If identifier is non-empty, a temporary overlay compose file is used to add
// the label `io.dockform/<identifier>` to that service before hashing.
func (c *Client) ComposeConfigHash(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, service string, identifier string, inlineEnv []string) (string, error) {
	// Choose compose files (overlay or user files)
	chosenFiles := files
	if identifier != "" {
		if pth, err := c.buildLabeledProjectTemp(ctx, workingDir, files, profiles, envFiles, projectName, identifier, inlineEnv); err == nil && pth != "" {
			defer func() { _ = os.Remove(pth) }()
			chosenFiles = []string{pth}
		}
	}
	args := c.composeBaseArgs(chosenFiles, profiles, envFiles, projectName)
	args = append(args, "config", "--hash", service)
	var out string
	var err error
	if len(inlineEnv) > 0 {
		out, err = c.exec.RunInDirWithEnv(ctx, workingDir, inlineEnv, args...)
	} else {
		out, err = c.exec.RunInDir(ctx, workingDir, args...)
	}
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
		return "", apperr.New("dockercli.ComposeConfigHash", apperr.External, "unexpected compose hash output: %s", util.Truncate(trimmed, 200))
	}
	return fields[len(fields)-1], nil
}

// buildLabeledProjectTemp loads the effective compose yaml via `docker compose config`,
// injects io.dockform/<identifier> label into all services, writes to a temp file, and returns its path.
func (c *Client) buildLabeledProjectTemp(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, identifier string, inlineEnv []string) (string, error) {
	if identifier == "" {
		return "", nil
	}
	args := c.composeBaseArgs(files, profiles, envFiles, projectName)
	args = append(args, "config")
	var out string
	var err error
	if len(inlineEnv) > 0 {
		out, err = c.exec.RunInDirWithEnv(ctx, workingDir, inlineEnv, args...)
	} else {
		out, err = c.exec.RunInDir(ctx, workingDir, args...)
	}
	if err != nil {
		return "", err
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		return "", apperr.Wrap("dockercli.buildLabeledProjectTemp", apperr.Internal, err, "parse compose yaml")
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
		labels["io.dockform/"+identifier] = "1"
		service["labels"] = labels
		services[name] = service
	}
	doc["services"] = services
	b, err := yaml.Marshal(doc)
	if err != nil {
		return "", apperr.Wrap("dockercli.buildLabeledProjectTemp", apperr.Internal, err, "marshal labeled yaml")
	}
	f, err := os.CreateTemp("", "dockform-labeled-project-*.yml")
	if err != nil {
		return "", apperr.Wrap("dockercli.buildLabeledProjectTemp", apperr.Internal, err, "create temp project")
	}
	path := f.Name()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", apperr.Wrap("dockercli.buildLabeledProjectTemp", apperr.Internal, err, "write temp project")
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
