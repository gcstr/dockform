package common

import (
	"context"
	"os"
	"strings"

	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/sshmux"
	"github.com/spf13/cobra"
)

type sshMuxKey struct{}

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
// and at least one context is remote. Best-effort: on any failure it silently falls back to
// the current per-call behavior. The Manager is stashed in cmd's context for
// TeardownSSHMux to retrieve.
func ActivateSSHMux(cmd *cobra.Command, cfg *manifest.Config, transport SSHTransport) {
	if transport != SSHTransportMux || !HasRemoteContext(cfg) {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	mgr, err := sshmux.Setup(exe)
	if err != nil {
		return
	}
	root := cmd.Root()
	root.SetContext(context.WithValue(root.Context(), sshMuxKey{}, mgr))
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
