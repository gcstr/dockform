package common

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/spf13/cobra"
)

// SSHTransport selects how dockform reaches Docker over ssh:// contexts.
type SSHTransport string

const (
	// SSHTransportTunnel forwards the remote Docker socket over one SSH
	// connection per host (see package sshtunnel). Immune to sshd MaxSessions.
	SSHTransportTunnel SSHTransport = "tunnel"
	// SSHTransportMux reuses one ControlMaster connection per host for the run.
	SSHTransportMux SSHTransport = "mux"
	// SSHTransportDirect opens a new SSH connection for every docker call. Kept
	// for compatibility; it is the least reliable option under concurrency.
	SSHTransportDirect SSHTransport = "direct"
)

// DefaultSSHTransport applies when neither a flag nor an environment variable
// selects a transport.
const DefaultSSHTransport = SSHTransportTunnel

const (
	envSSHTransport = "DOCKFORM_SSH_TRANSPORT"
	envSSHMultiplex = "DOCKFORM_SSH_MULTIPLEX" // deprecated alias
)

// ResolveSSHTransport returns the transport for this run. Precedence, highest
// first:
//
//  1. --ssh-transport, or the deprecated --ssh-multiplex
//  2. DOCKFORM_SSH_TRANSPORT, or the deprecated DOCKFORM_SSH_MULTIPLEX
//  3. DefaultSSHTransport
//
// At each level the new setting and its deprecated alias may both be present
// only if they agree. The returned warnings cover the deprecated environment
// variable; cobra reports the deprecated flag itself.
func ResolveSSHTransport(cmd *cobra.Command) (SSHTransport, []string, error) {
	var fromFlag, fromLegacyFlag SSHTransport
	if f := cmd.Flags().Lookup("ssh-transport"); f != nil && f.Changed {
		t, err := parseSSHTransport("--ssh-transport", f.Value.String())
		if err != nil {
			return "", nil, err
		}
		fromFlag = t
	}
	if f := cmd.Flags().Lookup("ssh-multiplex"); f != nil && f.Changed {
		on, _ := cmd.Flags().GetBool("ssh-multiplex")
		fromLegacyFlag = transportForMultiplex(on)
	}
	t, err := reconcileSSHTransport(fromFlag, fromLegacyFlag, "--ssh-transport", "--ssh-multiplex")
	if err != nil || t != "" {
		return t, nil, err
	}

	var warnings []string
	var fromEnv, fromLegacyEnv SSHTransport
	if raw := strings.TrimSpace(os.Getenv(envSSHTransport)); raw != "" {
		t, err := parseSSHTransport(envSSHTransport, raw)
		if err != nil {
			return "", nil, err
		}
		fromEnv = t
	}
	if raw := strings.TrimSpace(os.Getenv(envSSHMultiplex)); raw != "" {
		if on, err := strconv.ParseBool(raw); err == nil {
			fromLegacyEnv = transportForMultiplex(on)
			warnings = append(warnings, fmt.Sprintf("%s is deprecated; use %s=%s instead", envSSHMultiplex, envSSHTransport, fromLegacyEnv))
		} else {
			warnings = append(warnings, fmt.Sprintf("ignoring %s=%q: not a boolean; use %s instead", envSSHMultiplex, raw, envSSHTransport))
		}
	}
	t, err = reconcileSSHTransport(fromEnv, fromLegacyEnv, envSSHTransport, envSSHMultiplex)
	if err != nil {
		return "", nil, err
	}
	if t != "" {
		return t, warnings, nil
	}
	return DefaultSSHTransport, warnings, nil
}

func parseSSHTransport(source, raw string) (SSHTransport, error) {
	switch t := SSHTransport(strings.ToLower(strings.TrimSpace(raw))); t {
	case SSHTransportTunnel, SSHTransportMux, SSHTransportDirect:
		return t, nil
	}
	return "", apperr.New("common.ResolveSSHTransport", apperr.InvalidInput, "invalid %s %q: must be one of tunnel, mux, direct", source, raw)
}

func transportForMultiplex(on bool) SSHTransport {
	if on {
		return SSHTransportMux
	}
	return SSHTransportDirect
}

// reconcileSSHTransport picks the transport at one precedence level, or ""
// when that level sets nothing.
func reconcileSSHTransport(current, legacy SSHTransport, currentName, legacyName string) (SSHTransport, error) {
	switch {
	case current != "" && legacy != "" && current != legacy:
		return "", apperr.New("common.ResolveSSHTransport", apperr.InvalidInput,
			"%s=%s conflicts with %s, which means %s; set only %s", currentName, current, legacyName, legacy, currentName)
	case current != "":
		return current, nil
	default:
		return legacy, nil
	}
}
