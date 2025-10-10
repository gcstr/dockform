package dockercli

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// StreamContainerLogs streams logs for a container to w until ctx is canceled.
// tail specifies how many lines to include initially (0 = default). since is optional RFC3339 timestamp.
func (c *Client) StreamContainerLogs(ctx context.Context, name string, tail int, since string, w io.Writer) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	args := []string{"logs", "--follow"}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprint(tail))
	}
	if strings.TrimSpace(since) != "" {
		args = append(args, "--since", since)
	}
	args = append(args, name)
	return c.exec.RunWithStdout(ctx, w, args...)
}
