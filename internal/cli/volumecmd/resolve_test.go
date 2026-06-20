package volumecmd

import (
	"testing"

	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/manifest"
)

func ctxWith(names ...string) *common.CLIContext {
	cfg := &manifest.Config{Identifier: "demo", Contexts: map[string]manifest.ContextConfig{}}
	for _, n := range names {
		cfg.Contexts[n] = manifest.ContextConfig{}
	}
	return &common.CLIContext{Config: cfg, Factory: common.CreateClientFactory()}
}

func TestResolveVolumeTarget(t *testing.T) {
	// context/volume form resolves both parts and a client.
	c, v, cl, err := resolveVolumeTarget(ctxWith("a", "b"), "b/netbird_data")
	if err != nil || c != "b" || v != "netbird_data" || cl == nil {
		t.Fatalf("context/volume: got c=%q v=%q cl=%v err=%v", c, v, cl, err)
	}

	// Unknown context errors.
	if _, _, _, err := resolveVolumeTarget(ctxWith("a", "b"), "x/vol"); err == nil {
		t.Fatal("expected error for unknown context")
	}

	// Bare name with multiple contexts is ambiguous -> error mentioning the form.
	if _, _, _, err := resolveVolumeTarget(ctxWith("a", "b"), "vol"); err == nil {
		t.Fatal("expected error for ambiguous bare volume name")
	}

	// Bare name with a single context resolves to that context.
	c, v, _, err = resolveVolumeTarget(ctxWith("only"), "vol")
	if err != nil || c != "only" || v != "vol" {
		t.Fatalf("single context: got c=%q v=%q err=%v", c, v, err)
	}

	// Empty volume name errors.
	if _, _, _, err := resolveVolumeTarget(ctxWith("a"), "a/"); err == nil {
		t.Fatal("expected error for empty volume name")
	}
	// Empty context errors.
	if _, _, _, err := resolveVolumeTarget(ctxWith("a"), "/vol"); err == nil {
		t.Fatal("expected error for empty context name")
	}
}
