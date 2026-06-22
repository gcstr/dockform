package common

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/sshmux"
	"github.com/spf13/cobra"
)

type sshMuxKey struct{}

// MultiplexEnabled resolves whether SSH multiplexing is on. Precedence: an
// explicitly-set --ssh-multiplex flag wins; otherwise DOCKFORM_SSH_MULTIPLEX
// (when parseable) decides; otherwise it defaults to true.
func MultiplexEnabled(cmd *cobra.Command) bool {
	if f := cmd.Flags().Lookup("ssh-multiplex"); f != nil && f.Changed {
		v, _ := cmd.Flags().GetBool("ssh-multiplex")
		return v
	}
	if raw, ok := os.LookupEnv("DOCKFORM_SSH_MULTIPLEX"); ok {
		if v, err := strconv.ParseBool(strings.TrimSpace(raw)); err == nil {
			return v
		}
	}
	return true
}

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

// ActivateSSHMux installs run-scoped SSH multiplexing when enabled and at least
// one context is remote. Best-effort: on any failure it silently falls back to
// the current per-call behavior. The Manager is stashed in cmd's context for
// TeardownSSHMux to retrieve.
func ActivateSSHMux(cmd *cobra.Command, cfg *manifest.Config) {
	if !MultiplexEnabled(cmd) || !HasRemoteContext(cfg) {
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
	cmd.SetContext(context.WithValue(cmd.Context(), sshMuxKey{}, mgr))
}

// TeardownSSHMux tears down any multiplexing installed for this command.
func TeardownSSHMux(cmd *cobra.Command) {
	if cmd == nil || cmd.Context() == nil {
		return
	}
	if mgr, ok := cmd.Context().Value(sshMuxKey{}).(*sshmux.Manager); ok {
		mgr.Teardown()
	}
}
