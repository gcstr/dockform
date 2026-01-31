package dockercli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/util"
)

// HelperImage is the Docker image used for file operations on volumes
const HelperImage = "alpine:3.22"

// Client provides higher-level helpers around docker CLI.
type Client struct {
	exec        Exec
	identifier  string
	contextName string

	composeCache   map[string]ComposeConfigDoc
	composeCacheMu sync.RWMutex
}

func New(contextName string) *Client {
	return &Client{exec: SystemExec{ContextName: contextName}, contextName: contextName}
}

// WithIdentifier sets an optional label identifier to scope discovery.
func (c *Client) WithIdentifier(id string) *Client {
	c.identifier = id
	return c
}

func (c *Client) loadComposeCache(key string) (ComposeConfigDoc, bool) {
	c.composeCacheMu.RLock()
	defer c.composeCacheMu.RUnlock()
	if c.composeCache == nil {
		return ComposeConfigDoc{}, false
	}
	doc, ok := c.composeCache[key]
	return doc, ok
}

func (c *Client) storeComposeCache(key string, doc ComposeConfigDoc) {
	c.composeCacheMu.Lock()
	defer c.composeCacheMu.Unlock()
	if c.composeCache == nil {
		c.composeCache = make(map[string]ComposeConfigDoc)
	}
	c.composeCache[key] = doc
}

// CheckDaemon verifies the docker daemon for the configured context is reachable.
func (c *Client) CheckDaemon(ctx context.Context) error {
	if _, err := c.exec.Run(ctx, "version", "--format", "{{.Server.Version}}"); err != nil {
		// Check if this is a context cancellation error - if so, return it directly
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if c.contextName != "" && c.contextName != "default" {
			return apperr.Wrap("dockercli.CheckDaemon", apperr.Unavailable, err, "docker daemon not reachable (context=%s)", c.contextName)
		}
		return apperr.Wrap("dockercli.CheckDaemon", apperr.Unavailable, err, "docker daemon not reachable")
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

// RestartContainer restarts a container by name.
func (c *Client) RestartContainer(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return apperr.New("dockercli.RestartContainer", apperr.InvalidInput, "container name required")
	}
	_, err := c.exec.Run(ctx, "container", "restart", name)
	return err
}

// PauseContainer pauses a running container by name.
func (c *Client) PauseContainer(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return apperr.New("dockercli.PauseContainer", apperr.InvalidInput, "container name required")
	}
	_, err := c.exec.Run(ctx, "container", "pause", name)
	return err
}

// InspectContainerLabels returns selected labels from a container.
func (c *Client) InspectContainerLabels(ctx context.Context, containerName string, keys []string) (map[string]string, error) {
	if containerName == "" {
		return nil, apperr.New("dockercli.InspectContainerLabels", apperr.InvalidInput, "container name required")
	}
	out, err := c.exec.Run(ctx, "inspect", "-f", "{{json .Config.Labels}}", containerName)
	if err != nil {
		return nil, err
	}
	var labels map[string]string
	if err := json.Unmarshal([]byte(out), &labels); err != nil {
		return nil, apperr.Wrap("dockercli.InspectContainerLabels", apperr.Internal, err, "parse labels json")
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

// InspectMultipleContainerLabels returns selected labels from multiple containers in a single call
func (c *Client) InspectMultipleContainerLabels(ctx context.Context, containerNames []string, keys []string) (map[string]map[string]string, error) {
	if len(containerNames) == 0 {
		return nil, nil
	}

	// Build the inspect command for multiple containers
	args := append([]string{"inspect", "-f", "{{.Name}}\t{{json .Config.Labels}}"}, containerNames...)
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}

	result := make(map[string]map[string]string)
	lines := strings.Split(strings.TrimSpace(out), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}

		containerName := strings.TrimPrefix(parts[0], "/") // Remove leading slash
		var labels map[string]string
		if err := json.Unmarshal([]byte(parts[1]), &labels); err != nil {
			continue // Skip containers with parse errors
		}

		// Filter to requested keys if specified
		if len(keys) > 0 {
			filtered := make(map[string]string)
			for _, k := range keys {
				if v, ok := labels[k]; ok {
					filtered[k] = v
				}
			}
			labels = filtered
		}

		result[containerName] = labels
	}

	return result, nil
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
		args = append(args, "--filter", "label=io.dockform.identifier="+c.identifier)
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
// - Ensure destination exists in the helper container (mkdir -p)
// - Mount the volume at a fixed path in the helper container and operate there
// - Remove current contents then extract tar stream into targetPath
func (c *Client) SyncDirToVolume(ctx context.Context, volumeName, targetPath, localDir string) error {
	if volumeName == "" {
		return apperr.New("dockercli.SyncDirToVolume", apperr.InvalidInput, "volume name required")
	}
	if !strings.HasPrefix(targetPath, "/") {
		return apperr.New("dockercli.SyncDirToVolume", apperr.InvalidInput, "targetPath must be absolute")
	}
	if targetPath == "/" {
		return apperr.New("dockercli.SyncDirToVolume", apperr.InvalidInput, "targetPath cannot be '/'")
	}
	// Mount the volume at a fixed, known path to avoid quoting user-supplied targetPath in shell
	const dst = "/.dst"
	cmd := []string{
		"run", "--rm", "-i",
		"-v", fmt.Sprintf("%s:%s", volumeName, dst),
		HelperImage, "sh", "-c",
		"mkdir -p '" + dst + "' && rm -rf '" + dst + "'/* '" + dst + "'/.[!.]* '" + dst + "'/..?* 2>/dev/null || true; tar -xpf - -C '" + dst + "'",
	}
	pr, pw := io.Pipe()
	go func() {
		werr := util.TarDirectoryToWriter(localDir, "", pw)
		_ = pw.CloseWithError(werr)
	}()
	_, err := c.exec.RunWithStdin(ctx, pr, cmd...)
	return err
}

// normalizeVolumeMountPath returns a safe mount path for volumes.
// Docker doesn't allow mounting at /, so we use /data instead.
func normalizeVolumeMountPath(targetPath string) string {
	if targetPath == "/" {
		return "/data"
	}
	return targetPath
}

// ReadFileFromVolume returns the contents of a file inside a mounted volume target path.
// If the file does not exist, it returns an empty string and no error.
func (c *Client) ReadFileFromVolume(ctx context.Context, volumeName, targetPath, relFile string) (string, error) {
	if volumeName == "" || !strings.HasPrefix(targetPath, "/") {
		return "", apperr.New("dockercli.ReadFileFromVolume", apperr.InvalidInput, "invalid volume or target path")
	}
	mountPath := normalizeVolumeMountPath(targetPath)
	full := path.Join(mountPath, relFile)
	cmd := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:%s", volumeName, mountPath),
		HelperImage, "sh", "-c",
		"cat '" + full + "' 2>/dev/null || true",
	}
	out, err := c.exec.Run(ctx, cmd...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\r\n"), nil
}

// WriteFileToVolume writes content to a file inside a mounted volume target path, creating parent directories.
func (c *Client) WriteFileToVolume(ctx context.Context, volumeName, targetPath, relFile, content string) error {
	if volumeName == "" || !strings.HasPrefix(targetPath, "/") {
		return apperr.New("dockercli.WriteFileToVolume", apperr.InvalidInput, "invalid volume or target path")
	}
	mountPath := normalizeVolumeMountPath(targetPath)
	full := path.Join(mountPath, relFile)
	dir := path.Dir(full)
	cmd := []string{
		"run", "--rm", "-i",
		"-v", fmt.Sprintf("%s:%s", volumeName, mountPath),
		HelperImage, "sh", "-c",
		"mkdir -p '" + dir + "' && cat > '" + full + "'",
	}
	_, err := c.exec.RunWithStdin(ctx, strings.NewReader(content), cmd...)
	return err
}

// ExtractTarToVolume extracts a tar stream (stdin) into the volume targetPath without clearing existing files.
// It ensures targetPath exists.
func (c *Client) ExtractTarToVolume(ctx context.Context, volumeName, targetPath string, r io.Reader) error {
	if volumeName == "" || !strings.HasPrefix(targetPath, "/") {
		return apperr.New("dockercli.ExtractTarToVolume", apperr.InvalidInput, "invalid volume or target path")
	}
	mountPath := normalizeVolumeMountPath(targetPath)
	cmd := []string{
		"run", "--rm", "-i",
		"-v", fmt.Sprintf("%s:%s", volumeName, mountPath),
		HelperImage, "sh", "-c",
		"mkdir -p '" + mountPath + "' && tar -xpf - -C '" + mountPath + "'",
	}
	_, err := c.exec.RunWithStdin(ctx, r, cmd...)
	return err
}

// RemovePathsFromVolume removes one or more relative paths from the mounted targetPath.
func (c *Client) RemovePathsFromVolume(ctx context.Context, volumeName, targetPath string, relPaths []string) error {
	if volumeName == "" || !strings.HasPrefix(targetPath, "/") {
		return apperr.New("dockercli.RemovePathsFromVolume", apperr.InvalidInput, "invalid volume or target path")
	}
	if len(relPaths) == 0 {
		return nil
	}
	mountPath := normalizeVolumeMountPath(targetPath)
	// Build a safe rm command using printf with NUL and xargs -0 to avoid globbing
	printfArgs := strings.Builder{}
	for _, p := range relPaths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		full := path.Join(mountPath, p)
		printfArgs.WriteString(full)
		printfArgs.WriteByte('\x00')
	}
	cmd := []string{
		"run", "--rm", "-i",
		"-v", fmt.Sprintf("%s:%s", volumeName, mountPath),
		HelperImage, "sh", "-eu", "-c",
		"xargs -0 rm -rf -- 2>/dev/null || true",
	}
	_, err := c.exec.RunWithStdin(ctx, strings.NewReader(printfArgs.String()), cmd...)
	return err
}

// VolumeScriptResult contains the output from a volume script execution.
type VolumeScriptResult struct {
	Stdout string
	Stderr string
}

// RunVolumeScript executes a shell script inside a helper container with the specified volume mounted.
// The volume is mounted at targetPath (e.g., /app), matching where files were synced.
func (c *Client) RunVolumeScript(ctx context.Context, volumeName, targetPath, script string, env []string) (VolumeScriptResult, error) {
	if volumeName == "" {
		return VolumeScriptResult{}, apperr.New("dockercli.RunVolumeScript", apperr.InvalidInput, "volume name required")
	}
	if !strings.HasPrefix(targetPath, "/") {
		return VolumeScriptResult{}, apperr.New("dockercli.RunVolumeScript", apperr.InvalidInput, "target path must be absolute")
	}
	if strings.TrimSpace(script) == "" {
		return VolumeScriptResult{}, apperr.New("dockercli.RunVolumeScript", apperr.InvalidInput, "script cannot be empty")
	}

	// Build docker run command
	cmd := []string{"run", "--rm"}

	// Add environment variables
	for _, e := range env {
		if strings.TrimSpace(e) != "" {
			cmd = append(cmd, "-e", e)
		}
	}

	// Mount volume at targetPath (same as ExtractTarToVolume does)
	mountPath := normalizeVolumeMountPath(targetPath)
	cmd = append(cmd, "-v", fmt.Sprintf("%s:%s", volumeName, mountPath))

	// Use helper image and run script with sh
	cmd = append(cmd, HelperImage, "sh", "-c", script)

	// Execute command using RunDetailed to capture both stdout and stderr
	res, err := c.exec.RunDetailed(ctx, Options{}, cmd...)
	if err != nil {
		return VolumeScriptResult{Stdout: res.Stdout, Stderr: res.Stderr}, err
	}

	return VolumeScriptResult{Stdout: res.Stdout, Stderr: res.Stderr}, nil
}

// RunInHelperImage executes a command in the helper image without mounting volumes.
// Useful for checking if binaries are available.
func (c *Client) RunInHelperImage(ctx context.Context, script string) (string, error) {
	if strings.TrimSpace(script) == "" {
		return "", apperr.New("dockercli.RunInHelperImage", apperr.InvalidInput, "script cannot be empty")
	}

	cmd := []string{"run", "--rm", HelperImage, "sh", "-c", script}
	out, err := c.exec.Run(ctx, cmd...)
	return out, err
}
