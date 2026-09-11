package common

import (
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/sshmux"
	"github.com/spf13/cobra"
)

// newTransportCmd registers both flags the way the root command does.
func newTransportCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("ssh-transport", "mux", "")
	cmd.Flags().Bool("ssh-multiplex", true, "")
	return cmd
}

func TestResolveSSHTransport(t *testing.T) {
	cases := []struct {
		name     string
		flags    map[string]string
		env      map[string]string
		want     SSHTransport
		wantErr  bool
		wantWarn bool
	}{
		{name: "default is tunnel", want: SSHTransportTunnel},
		{name: "flag direct", flags: map[string]string{"ssh-transport": "direct"}, want: SSHTransportDirect},
		{name: "flag is case-insensitive and trimmed", flags: map[string]string{"ssh-transport": " MUX "}, want: SSHTransportMux},
		{name: "flag rejects unknown value", flags: map[string]string{"ssh-transport": "bogus"}, wantErr: true},
		{name: "flag tunnel", flags: map[string]string{"ssh-transport": "tunnel"}, want: SSHTransportTunnel},
		{name: "legacy flag false means direct", flags: map[string]string{"ssh-multiplex": "false"}, want: SSHTransportDirect},
		{name: "legacy flag true means mux", flags: map[string]string{"ssh-multiplex": "true"}, want: SSHTransportMux},
		{name: "both flags agreeing is fine", flags: map[string]string{"ssh-transport": "direct", "ssh-multiplex": "false"}, want: SSHTransportDirect},
		{name: "both flags disagreeing is an error", flags: map[string]string{"ssh-transport": "mux", "ssh-multiplex": "false"}, wantErr: true},
		{name: "env transport", env: map[string]string{"DOCKFORM_SSH_TRANSPORT": "direct"}, want: SSHTransportDirect},
		{name: "env transport rejects unknown value", env: map[string]string{"DOCKFORM_SSH_TRANSPORT": "bogus"}, wantErr: true},
		{name: "legacy env false means direct and warns", env: map[string]string{"DOCKFORM_SSH_MULTIPLEX": "false"}, want: SSHTransportDirect, wantWarn: true},
		{name: "legacy env unparseable is ignored with a warning", env: map[string]string{"DOCKFORM_SSH_MULTIPLEX": "garbage"}, want: SSHTransportTunnel, wantWarn: true},
		{name: "both envs disagreeing is an error", env: map[string]string{"DOCKFORM_SSH_TRANSPORT": "mux", "DOCKFORM_SSH_MULTIPLEX": "false"}, wantErr: true},
		{name: "flag beats env", flags: map[string]string{"ssh-transport": "mux"}, env: map[string]string{"DOCKFORM_SSH_TRANSPORT": "direct"}, want: SSHTransportMux},
		{name: "legacy flag beats new env", flags: map[string]string{"ssh-multiplex": "false"}, env: map[string]string{"DOCKFORM_SSH_TRANSPORT": "mux"}, want: SSHTransportDirect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Empty means unset, and isolates the case from the developer's shell.
			t.Setenv("DOCKFORM_SSH_TRANSPORT", "")
			t.Setenv("DOCKFORM_SSH_MULTIPLEX", "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cmd := newTransportCmd()
			for k, v := range tc.flags {
				if err := cmd.Flags().Set(k, v); err != nil {
					t.Fatalf("set --%s=%s: %v", k, v, err)
				}
			}

			got, warnings, err := ResolveSSHTransport(cmd)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got transport %q", got)
				}
				if !apperr.IsKind(err, apperr.InvalidInput) {
					t.Errorf("error should be InvalidInput (exit code 2), got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("transport = %q, want %q", got, tc.want)
			}
			if tc.wantWarn && len(warnings) == 0 {
				t.Errorf("expected a warning, got none")
			}
			if !tc.wantWarn && len(warnings) != 0 {
				t.Errorf("expected no warnings, got %v", warnings)
			}
		})
	}
}

// Commands that never registered the flags (tests, subcommand helpers) must
// still resolve to the default rather than panic.
func TestResolveSSHTransport_FlagsNotRegistered(t *testing.T) {
	t.Setenv("DOCKFORM_SSH_TRANSPORT", "")
	t.Setenv("DOCKFORM_SSH_MULTIPLEX", "")
	got, _, err := ResolveSSHTransport(&cobra.Command{})
	if err != nil || got != SSHTransportTunnel {
		t.Fatalf("got %q, %v; want tunnel, nil", got, err)
	}
}

// direct means no ControlMaster: ActivateSSHMux must not install the shim.
func TestActivateSSHMux_DirectDoesNotInstall(t *testing.T) {
	root := &cobra.Command{Use: "dockform"}
	cmd := &cobra.Command{Use: "plan"}
	root.AddCommand(cmd)
	root.SetContext(t.Context())
	cfg := &manifest.Config{Contexts: map[string]manifest.ContextConfig{"remote": {}}}

	t.Setenv(sshmux.ControlEnvVar, "")
	ActivateSSHMux(cmd, cfg, SSHTransportDirect)
	defer TeardownSSHMux(cmd)

	if v := root.Context().Value(sshMuxKey{}); v != nil {
		t.Fatal("direct transport must not install SSH multiplexing")
	}
}

// The dashboard is a long-running TUI and keeps the self-healing multiplexer.
func TestEffectiveSSHTransport_DashboardNeverTunnels(t *testing.T) {
	dash := &cobra.Command{Use: "dashboard", Annotations: map[string]string{AnnotationSSHTunnel: "off"}}
	if got := EffectiveSSHTransport(dash, SSHTransportTunnel); got != SSHTransportMux {
		t.Errorf("dashboard + tunnel = %q, want mux", got)
	}
	if got := EffectiveSSHTransport(dash, SSHTransportDirect); got != SSHTransportDirect {
		t.Errorf("dashboard + direct = %q, want direct (still honoured)", got)
	}
	if got := EffectiveSSHTransport(&cobra.Command{Use: "plan"}, SSHTransportTunnel); got != SSHTransportTunnel {
		t.Errorf("plan + tunnel = %q, want tunnel", got)
	}
}
