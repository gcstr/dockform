package common

import (
	"context"
	"os"
	"strings"

	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/sshmux"
	"github.com/spf13/cobra"
)

type sshMuxKey struct{}

// setupSSHMux is sshmux.Setup, swappable in tests.
var setupSSHMux = sshmux.Setup

// HasRemoteContext reports whether any context could use SSH (an ssh:// host
// override, or a named non-default context).
func HasRemoteContext(cfg *manifest.Config) bool {
	for name, cc := range cfg.Contexts {
		if strings.HasPrefix(cc.Host, "ssh://") {
			return true
		}
		if name != "" && name != "default" {
			return true
		}
	}
	return false
}

// ActivateSSHMux installs run-scoped SSH multiplexing when the transport is mux
// and at least one context is remote. If it cannot be installed the run falls
// back to one SSH connection per docker call and logs a warning saying so: that
// fallback changes which sshd limit binds, so it must never happen silently.
// The Manager is stashed in cmd's context for TeardownSSHMux to retrieve.
func ActivateSSHMux(cmd *cobra.Command, cfg *manifest.Config, transport SSHTransport) {
	if transport != SSHTransportMux || !HasRemoteContext(cfg) {
		return
	}
	log := logger.FromContext(cmd.Context()).With("component", "sshmux")
	exe, err := os.Executable()
	if err == nil {
		var mgr *sshmux.Manager
		mgr, err = setupSSHMux(exe)
		if err == nil {
			log.Debug("ssh_mux_active", "dir", mgr.Dir())
			root := cmd.Root()
			root.SetContext(context.WithValue(root.Context(), sshMuxKey{}, mgr))
			return
		}
	}
	log.Warn("ssh_mux_unavailable", "error", err,
		"impact", "each docker call opens its own SSH connection; large stacks may hit sshd MaxStartups")
}

// TeardownSSHMux tears down any multiplexing installed for this command.
func TeardownSSHMux(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	root := cmd.Root()
	if root.Context() == nil {
		return
	}
	if mgr, ok := root.Context().Value(sshMuxKey{}).(*sshmux.Manager); ok {
		mgr.Teardown()
	}
}
