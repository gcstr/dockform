package dockercli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

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
			return apperr.Wrap("dockercli.CheckDaemon", apperr.Unavailable, err, "docker daemon not reachable (context=%s): %v", c.contextName, err)
		}
		return apperr.Wrap("dockercli.CheckDaemon", apperr.Unavailable, err, "docker daemon not reachable: %v", err)
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

// ReadFileFromVolume returns the contents of a file inside a mounted volume target path.
// If the file does not exist, it returns an empty string and no error.
func (c *Client) ReadFileFromVolume(ctx context.Context, volumeName, targetPath, relFile string) (string, error) {
	if volumeName == "" || !strings.HasPrefix(targetPath, "/") {
		return "", apperr.New("dockercli.ReadFileFromVolume", apperr.InvalidInput, "invalid volume or target path")
	}
	full := path.Join(targetPath, relFile)
	cmd := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:%s", volumeName, targetPath),
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
	full := path.Join(targetPath, relFile)
	dir := path.Dir(full)
	cmd := []string{
		"run", "--rm", "-i",
		"-v", fmt.Sprintf("%s:%s", volumeName, targetPath),
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
	cmd := []string{
		"run", "--rm", "-i",
		"-v", fmt.Sprintf("%s:%s", volumeName, targetPath),
		HelperImage, "sh", "-c",
		"mkdir -p '" + targetPath + "' && tar -xpf - -C '" + targetPath + "'",
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
	// Build a safe rm command using printf with NUL and xargs -0 to avoid globbing
	printfArgs := strings.Builder{}
	for _, p := range relPaths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		full := path.Join(targetPath, p)
		printfArgs.WriteString(full)
		printfArgs.WriteByte('\x00')
	}
	cmd := []string{
		"run", "--rm", "-i",
		"-v", fmt.Sprintf("%s:%s", volumeName, targetPath),
		HelperImage, "sh", "-eu", "-c",
		"xargs -0 rm -rf -- 2>/dev/null || true",
	}
	_, err := c.exec.RunWithStdin(ctx, strings.NewReader(printfArgs.String()), cmd...)
	return err
}
