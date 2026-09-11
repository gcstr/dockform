package common

import (
	"context"
	"os"
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/sshmux"
	"github.com/spf13/cobra"
)

func TestHasRemoteContext(t *testing.T) {
	cases := []struct {
		name string
		cfg  manifest.Config
		want bool
	}{
		{"only default", manifest.Config{Contexts: map[string]manifest.ContextConfig{"default": {}}}, false},
		{"ssh host override", manifest.Config{Contexts: map[string]manifest.ContextConfig{"default": {Host: "ssh://u@h"}}}, true},
		{"named non-default", manifest.Config{Contexts: map[string]manifest.ContextConfig{"hetzner": {}}}, true},
		{"empty", manifest.Config{Contexts: map[string]manifest.ContextConfig{}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasRemoteContext(&tc.cfg); got != tc.want {
				t.Fatalf("HasRemoteContext = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestActivateTeardownRoundTrip_RemovesRunDir(t *testing.T) {
	// Mirror the real wiring: ActivateSSHMux runs inside a leaf subcommand's
	// RunE, TeardownSSHMux runs on the root command (as root.Execute does).
	root := &cobra.Command{Use: "dockform"}
	root.PersistentFlags().Bool("ssh-multiplex", true, "")

	var capturedDir string
	leaf := &cobra.Command{
		Use: "plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := &manifest.Config{Contexts: map[string]manifest.ContextConfig{"remote": {}}}
			ActivateSSHMux(cmd, cfg, SSHTransportMux)
			capturedDir = os.Getenv(sshmux.ControlEnvVar)
			return nil
		},
	}
	root.AddCommand(leaf)

	oldPath := os.Getenv("PATH")
	root.SetArgs([]string{"plan"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if capturedDir == "" {
		t.Fatal("ActivateSSHMux did not install multiplexing for a remote context")
	}
	if _, err := os.Stat(capturedDir); err != nil {
		t.Fatalf("expected run dir to exist during run: %v", err)
	}

	// Teardown on the ROOT command, exactly as root.Execute does.
	TeardownSSHMux(root)

	if _, err := os.Stat(capturedDir); !os.IsNotExist(err) {
		t.Fatalf("run dir not removed after teardown (manager not found on root context): %v", err)
	}
	if os.Getenv("PATH") != oldPath {
		t.Errorf("PATH not restored after teardown")
	}
}
