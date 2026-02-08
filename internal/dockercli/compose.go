package dockercli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/util"
	"github.com/goccy/go-yaml"
)

// runInDirOptionalEnv runs a command in workingDir, adding inlineEnv to the
// process environment only when the slice is non-empty.
func (c *Client) runInDirOptionalEnv(ctx context.Context, workingDir string, inlineEnv []string, args ...string) (string, error) {
	if len(inlineEnv) > 0 {
		return c.exec.RunInDirWithEnv(ctx, workingDir, inlineEnv, args...)
	}
	return c.exec.RunInDir(ctx, workingDir, args...)
}

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

	return c.runInDirOptionalEnv(ctx, workingDir, inlineEnv, args...)
}

// ComposeConfigServices returns the list of service names that would be part of the project.
func (c *Client) ComposeConfigServices(ctx context.Context, workingDir string, files, profiles, envFiles []string, inlineEnv []string) ([]string, error) {
	args := c.composeBaseArgs(files, profiles, envFiles, "")
	args = append(args, "config", "--services")
	out, err := c.runInDirOptionalEnv(ctx, workingDir, inlineEnv, args...)
	if err != nil {
		return nil, err
	}
	return util.SplitNonEmptyLines(out), nil
}

// ComposeConfigFull renders the effective compose config and parses desired services info (image, etc.).
func (c *Client) ComposeConfigFull(ctx context.Context, workingDir string, files, profiles, envFiles []string, inlineEnv []string) (ComposeConfigDoc, error) {
	cacheKey := c.composeCacheKey(workingDir, files, profiles, envFiles, inlineEnv)
	if doc, ok := c.loadComposeCache(cacheKey); ok {
		return doc, nil
	}
	args := c.composeBaseArgs(files, profiles, envFiles, "")
	// Prefer JSON when available
	argsJSON := append(append([]string{}, args...), "config", "--format", "json")
	out, err := c.runInDirOptionalEnv(ctx, workingDir, inlineEnv, argsJSON...)
	if err == nil {
		var doc ComposeConfigDoc
		if json.Unmarshal([]byte(out), &doc) == nil {
			c.storeComposeCache(cacheKey, doc)
			return doc, nil
		}
	}
	// Fallback to YAML
	argsYAML := append(append([]string{}, args...), "config")
	out, err = c.runInDirOptionalEnv(ctx, workingDir, inlineEnv, argsYAML...)
	if err != nil {
		return ComposeConfigDoc{}, apperr.Wrap("dockercli.ComposeConfigFull", apperr.Internal, err, "parse compose yaml")
	}
	var doc ComposeConfigDoc
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		return ComposeConfigDoc{}, apperr.Wrap("dockercli.ComposeConfigFull", apperr.Internal, err, "parse compose yaml")
	}
	c.storeComposeCache(cacheKey, doc)
	return doc, nil
}

// ComposeConfigRaw returns the fully resolved compose configuration as YAML text.
// It is equivalent to running `docker compose config` with the provided files,
// profiles and env files, resolved relative to workingDir. Inline environment
// variables are provided via the process environment for the command.
func (c *Client) ComposeConfigRaw(ctx context.Context, workingDir string, files, profiles, envFiles []string, inlineEnv []string) (string, error) {
	args := c.composeBaseArgs(files, profiles, envFiles, "")
	args = append(args, "config")
	return c.runInDirOptionalEnv(ctx, workingDir, inlineEnv, args...)
}

// ComposePs lists running (or created) compose services for the project.
func (c *Client) ComposePs(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, inlineEnv []string) ([]ComposePsItem, error) {
	args := c.composeBaseArgs(files, profiles, envFiles, projectName)
	args = append(args, "ps", "--format", "json")
	out, err := c.runInDirOptionalEnv(ctx, workingDir, inlineEnv, args...)
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
// the label `io.dockform.identifier: <identifier>` to that service before hashing.
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
	out, err := c.runInDirOptionalEnv(ctx, workingDir, inlineEnv, args...)
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

// ComposeConfigHashes returns compose config hashes for multiple services, reusing a single
// labeled overlay compose file when identifier is provided to avoid repeated `compose config`.
func (c *Client) ComposeConfigHashes(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, services []string, identifier string, inlineEnv []string) (map[string]string, error) {
	// Choose compose files (overlay or user files)
	chosenFiles := files
	if identifier != "" {
		if pth, err := c.buildLabeledProjectTemp(ctx, workingDir, files, profiles, envFiles, projectName, identifier, inlineEnv); err == nil && pth != "" {
			defer func() { _ = os.Remove(pth) }()
			chosenFiles = []string{pth}
		} else if err != nil {
			return nil, err
		}
	}
	base := c.composeBaseArgs(chosenFiles, profiles, envFiles, projectName)
	out := make(map[string]string, len(services))
	for _, svc := range services {
		args := append(append([]string{}, base...), "config", "--hash", svc)
		txt, err := c.runInDirOptionalEnv(ctx, workingDir, inlineEnv, args...)
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(txt)
		firstLine := trimmed
		if idx := strings.IndexAny(trimmed, "\r\n"); idx >= 0 {
			firstLine = trimmed[:idx]
		}
		fields := strings.Fields(firstLine)
		if len(fields) == 0 {
			return nil, apperr.New("dockercli.ComposeConfigHashes", apperr.External, "unexpected compose hash output: %s", util.Truncate(trimmed, 200))
		}
		out[svc] = fields[len(fields)-1]
	}
	return out, nil
}

func (c *Client) composeCacheKey(workingDir string, files, profiles, envFiles []string, inlineEnv []string) string {
	var b strings.Builder
	writePart := func(label string, vals []string) {
		b.WriteString(label)
		b.WriteString("=")
		b.WriteString(strings.Join(vals, ","))
		b.WriteString(";")
	}
	b.WriteString("dir=")
	b.WriteString(filepath.Clean(workingDir))
	b.WriteString(";")
	writePart("files", files)
	writePart("profiles", profiles)
	writePart("envfiles", envFiles)
	writePart("inline", inlineEnv)
	return b.String()
}

// buildLabeledProjectTemp loads the effective compose yaml via `docker compose config`,
// injects io.dockform.identifier=<identifier> label into all services, writes to a temp file, and returns its path.
func (c *Client) buildLabeledProjectTemp(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, identifier string, inlineEnv []string) (string, error) {
	if identifier == "" {
		return "", nil
	}
	args := c.composeBaseArgs(files, profiles, envFiles, projectName)
	args = append(args, "config")
	out, err := c.runInDirOptionalEnv(ctx, workingDir, inlineEnv, args...)
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
		labels["io.dockform.identifier"] = identifier
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
