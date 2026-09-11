package common

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/sshmux"
	"github.com/spf13/cobra"
)

// When multiplexing cannot be installed the run silently switches to one SSH
// connection per docker call, a different (and flakier) concurrency regime. The
// run must say so.
func TestActivateSSHMux_LogsWhenSetupFails(t *testing.T) {
	var buf bytes.Buffer
	l, closer, err := logger.New(logger.Options{Out: &buf, Format: "json", Level: "debug"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}
	orig := setupSSHMux
	setupSSHMux = func(string) (*sshmux.Manager, error) { return nil, errors.New("mkdtemp: read-only file system") }
	t.Cleanup(func() { setupSSHMux = orig })

	cmd := &cobra.Command{}
	cmd.SetContext(logger.WithContext(context.Background(), l))
	cfg := &manifest.Config{Contexts: map[string]manifest.ContextConfig{"hetzner": {}}}

	ActivateSSHMux(cmd, cfg, SSHTransportMux)

	out := buf.String()
	if !strings.Contains(out, "ssh_mux_unavailable") || !strings.Contains(out, "read-only file system") {
		t.Fatalf("expected a warning naming the failure, got logs: %s", out)
	}
}
