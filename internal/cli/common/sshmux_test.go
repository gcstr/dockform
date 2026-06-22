package common

import (
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
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

func newMuxCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("ssh-multiplex", true, "")
	return cmd
}

func TestMultiplexEnabled_Precedence(t *testing.T) {
	// Default: no flag change, no env → true.
	if !MultiplexEnabled(newMuxCmd()) {
		t.Fatal("default should be true")
	}

	// Env disables when no flag change.
	t.Setenv("DOCKFORM_SSH_MULTIPLEX", "false")
	if MultiplexEnabled(newMuxCmd()) {
		t.Fatal("env false should disable when flag unchanged")
	}

	// Explicit flag overrides env.
	cmd := newMuxCmd()
	_ = cmd.Flags().Set("ssh-multiplex", "true")
	if !MultiplexEnabled(cmd) {
		t.Fatal("explicit flag true should override env false")
	}

	// Unparseable env falls back to default true.
	t.Setenv("DOCKFORM_SSH_MULTIPLEX", "garbage")
	if !MultiplexEnabled(newMuxCmd()) {
		t.Fatal("unparseable env should fall back to default true")
	}
}
