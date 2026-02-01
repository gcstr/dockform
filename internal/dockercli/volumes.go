package dockercli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/util"
)

// ListVolumes returns names of docker volumes.
func (c *Client) ListVolumes(ctx context.Context) ([]string, error) {
	args := []string{"volume", "ls", "--format", "{{.Name}}"}
	if c.identifier != "" {
		args = append(args, "--filter", "label="+LabelIdentifier+"="+c.identifier)
	}
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return util.SplitNonEmptyLines(out), nil
}

// VolumeSummary contains basic metadata about a volume for dashboard display.
type VolumeSummary struct {
	Name       string
	Driver     string
	Mountpoint string
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

// VolumeDetails contains driver, options and labels of a volume.
type VolumeDetails struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Mountpoint string            `json:"Mountpoint"`
	Options    map[string]string `json:"Options"`
	Labels     map[string]string `json:"Labels"`
}

// InspectVolume returns driver/options/labels for a volume.
func (c *Client) InspectVolume(ctx context.Context, name string) (VolumeDetails, error) {
	if strings.TrimSpace(name) == "" {
		return VolumeDetails{}, apperr.New("dockercli.InspectVolume", apperr.InvalidInput, "volume name required")
	}
	// Request JSON for a single volume
	out, err := c.exec.Run(ctx, "volume", "inspect", "-f", "{{json .}}", name)
	if err != nil {
		return VolumeDetails{}, err
	}
	var d VolumeDetails
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		return VolumeDetails{}, apperr.Wrap("dockercli.InspectVolume", apperr.Internal, err, "parse inspect json")
	}
	if d.Options == nil {
		d.Options = map[string]string{}
	}
	if d.Labels == nil {
		d.Labels = map[string]string{}
	}
	return d, nil
}

// VolumeSummaries returns a list of volume metadata including driver and mountpoint.
func (c *Client) VolumeSummaries(ctx context.Context) ([]VolumeSummary, error) {
	args := []string{"volume", "ls", "--format", "{{.Name}}\t{{.Driver}}"}
	if c.identifier != "" {
		args = append(args, "--filter", "label="+LabelIdentifier+"="+c.identifier)
	}
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	lines := util.SplitNonEmptyLines(out)
	if len(lines) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(lines))
	driverByName := make(map[string]string, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		names = append(names, name)
		if len(parts) > 1 {
			driverByName[name] = strings.TrimSpace(parts[1])
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	inspectArgs := append([]string{"volume", "inspect"}, names...)
	inspectOut, err := c.exec.Run(ctx, inspectArgs...)
	if err != nil {
		return nil, err
	}
	var details []VolumeDetails
	if err := json.Unmarshal([]byte(inspectOut), &details); err != nil {
		return nil, apperr.Wrap("dockercli.VolumeSummaries", apperr.Internal, err, "parse inspect output")
	}
	summaries := make([]VolumeSummary, 0, len(details))
	for _, d := range details {
		name := strings.TrimSpace(d.Name)
		if name == "" {
			continue
		}
		driver := strings.TrimSpace(driverByName[name])
		if driver == "" {
			driver = strings.TrimSpace(d.Driver)
		}
		summaries = append(summaries, VolumeSummary{
			Name:       name,
			Driver:     driver,
			Mountpoint: strings.TrimSpace(d.Mountpoint),
		})
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Name < summaries[j].Name })
	return summaries, nil
}

// StreamTarFromVolume streams a tar of the root of the volume to w.
// The tar is created using numeric owners and includes xattrs/acls when supported.
func (c *Client) StreamTarFromVolume(ctx context.Context, volumeName string, w io.Writer) error {
	if strings.TrimSpace(volumeName) == "" {
		return apperr.New("dockercli.StreamTarFromVolume", apperr.InvalidInput, "volume name required")
	}
	// Use a fixed container path to avoid quoting user inputs
	const src = "/src"
	// Prefer GNU tar flags when available; fall back to minimal flags on BusyBox
	sh := "set -eo pipefail; if tar --version 2>/dev/null | grep -qi gnu; then TF='--numeric-owner --xattrs --acls'; else TF='--numeric-owner'; fi; tar $TF -C '" + src + "' -cf - ."
	cmd := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:%s:ro", volumeName, src),
		HelperImage, "sh", "-c", sh,
	}
	return c.exec.RunWithStdout(ctx, w, cmd...)
}

// StreamTarZstdFromVolume streams a zstd-compressed tar of the volume to w.
// It installs zstd in the helper container if missing (apk add).
func (c *Client) StreamTarZstdFromVolume(ctx context.Context, volumeName string, w io.Writer) error {
	if strings.TrimSpace(volumeName) == "" {
		return apperr.New("dockercli.StreamTarZstdFromVolume", apperr.InvalidInput, "volume name required")
	}
	const src = "/src"
	// Use pipefail so tar errors propagate; conditionally add xattrs/acls for GNU tar
	sh := "set -eo pipefail; apk add --no-cache zstd >/dev/null 2>&1 || true; if tar --version 2>/dev/null | grep -qi gnu; then TF='--numeric-owner --xattrs --acls'; else TF='--numeric-owner'; fi; tar $TF -C '" + src + "' -cf - . | zstd -q -z -T0 -19"
	cmd := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:%s:ro", volumeName, src),
		HelperImage, "sh", "-c", sh,
	}
	return c.exec.RunWithStdout(ctx, w, cmd...)
}

// IsVolumeEmpty returns true if the volume has no files (ignores . and ..).
func (c *Client) IsVolumeEmpty(ctx context.Context, volumeName string) (bool, error) {
	if strings.TrimSpace(volumeName) == "" {
		return false, apperr.New("dockercli.IsVolumeEmpty", apperr.InvalidInput, "volume name required")
	}
	const dst = "/dst"
	cmd := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:%s", volumeName, dst),
		HelperImage, "sh", "-c",
		"test -z \"$(ls -A '" + dst + "' 2>/dev/null)\" && echo empty || echo notempty",
	}
	out, err := c.exec.Run(ctx, cmd...)
	if err != nil {
		return false, err
	}
	out = strings.TrimSpace(out)
	return out == "empty", nil
}

// ClearVolume removes all contents of the volume's root directory.
func (c *Client) ClearVolume(ctx context.Context, volumeName string) error {
	if strings.TrimSpace(volumeName) == "" {
		return apperr.New("dockercli.ClearVolume", apperr.InvalidInput, "volume name required")
	}
	const dst = "/dst"
	cmd := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:%s", volumeName, dst),
		HelperImage, "sh", "-c",
		// Remove regular and dotfiles but not '.' or '..'
		"rm -rf '" + dst + "'/* '" + dst + "'/.[!.]* '" + dst + "'/..?* 2>/dev/null || true",
	}
	_, err := c.exec.Run(ctx, cmd...)
	return err
}

// ListContainersUsingVolume returns container names (running or stopped) that reference the volume.
func (c *Client) ListContainersUsingVolume(ctx context.Context, volumeName string) ([]string, error) {
	if strings.TrimSpace(volumeName) == "" {
		return nil, apperr.New("dockercli.ListContainersUsingVolume", apperr.InvalidInput, "volume name required")
	}
	args := []string{"ps", "-a", "--filter", "volume=" + volumeName, "--format", "{{.Names}}"}
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return util.SplitNonEmptyLines(out), nil
}

// ListRunningContainersUsingVolume returns names of running containers that reference the volume.
func (c *Client) ListRunningContainersUsingVolume(ctx context.Context, volumeName string) ([]string, error) {
	if strings.TrimSpace(volumeName) == "" {
		return nil, apperr.New("dockercli.ListRunningContainersUsingVolume", apperr.InvalidInput, "volume name required")
	}
	args := []string{"ps", "--filter", "volume=" + volumeName, "--format", "{{.Names}}"}
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return util.SplitNonEmptyLines(out), nil
}

// StopContainers stops the given containers gracefully.
func (c *Client) StopContainers(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	// Stop sequentially for clearer error surfacing
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			continue
		}
		if _, err := c.exec.Run(ctx, "container", "stop", n); err != nil {
			return err
		}
	}
	return nil
}

// StartContainers starts the given containers.
func (c *Client) StartContainers(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			continue
		}
		if _, err := c.exec.Run(ctx, "container", "start", n); err != nil {
			return err
		}
	}
	return nil
}

// TarStatsFromVolume calculates uncompressed tar size (bytes) and file count in the volume.
// It runs commands inside a helper container and returns (tarBytes, fileCount).
func (c *Client) TarStatsFromVolume(ctx context.Context, volumeName string) (int64, int64, error) {
	if strings.TrimSpace(volumeName) == "" {
		return 0, 0, apperr.New("dockercli.TarStatsFromVolume", apperr.InvalidInput, "volume name required")
	}
	const src = "/src"
	// Compute file count and tar byte size in one container invocation.
	// Use pipefail so a tar error propagates and is noticed by the caller.
	sh := "set -eo pipefail; fc=$(find '" + src + "' -xdev -type f 2>/dev/null | wc -l | tr -d '\r\n'); if tar --version 2>/dev/null | grep -qi gnu; then TF='--numeric-owner --xattrs --acls'; else TF='--numeric-owner'; fi; bytes=$(tar $TF -C '" + src + "' -cf - . | wc -c | tr -d '\r\n'); echo $fc $bytes"
	args := []string{"run", "--rm", "-v", fmt.Sprintf("%s:%s:ro", volumeName, src), HelperImage, "sh", "-c", sh}
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 2 {
		return 0, 0, apperr.New("dockercli.TarStatsFromVolume", apperr.External, "unexpected stats output: %s", util.Truncate(out, 200))
	}
	var fc64, bytes64 int64
	if v, perr := parseInt64(fields[0]); perr == nil {
		fc64 = v
	} else {
		return 0, 0, apperr.Wrap("dockercli.TarStatsFromVolume", apperr.Internal, perr, "parse file count")
	}
	if v, perr := parseInt64(fields[1]); perr == nil {
		bytes64 = v
	} else {
		return 0, 0, apperr.Wrap("dockercli.TarStatsFromVolume", apperr.Internal, perr, "parse bytes")
	}
	return bytes64, fc64, nil
}

// ExtractZstdTarToVolume reads a zstd-compressed tar from r and extracts into the volume root.
func (c *Client) ExtractZstdTarToVolume(ctx context.Context, volumeName string, r io.Reader) error {
	if strings.TrimSpace(volumeName) == "" {
		return apperr.New("dockercli.ExtractZstdTarToVolume", apperr.InvalidInput, "volume name required")
	}
	const dst = "/dst"
	cmd := []string{
		"run", "--rm", "-i",
		"-v", fmt.Sprintf("%s:%s", volumeName, dst),
		HelperImage, "sh", "-c",
		"apk add --no-cache zstd >/dev/null 2>&1 || true; mkdir -p '" + dst + "'; zstd -q -d -c | tar -xpf - -C '" + dst + "'",
	}
	_, err := c.exec.RunWithStdin(ctx, r, cmd...)
	return err
}

// parseInt64 parses a decimal string into int64.
func parseInt64(s string) (int64, error) {
	var n int64
	for _, ch := range []byte(strings.TrimSpace(s)) {
		if ch < '0' || ch > '9' {
			return 0, apperr.New("dockercli.parseInt64", apperr.InvalidInput, "invalid digit")
		}
		n = n*10 + int64(ch-'0')
	}
	return n, nil
}
