package common

import (
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
)

func multiContextConfig() *manifest.Config {
	return &manifest.Config{
		Identifier: "prod",
		Contexts: map[string]manifest.ContextConfig{
			"hetzner-one": {},
			"hetzner-two": {},
			"aws":         {},
		},
		Stacks: map[string]manifest.Stack{
			"hetzner-one/traefik":  {Root: "/h1/traefik"},
			"hetzner-one/app":      {Root: "/h1/app"},
			"hetzner-two/traefik":  {Root: "/h2/traefik"},
			"hetzner-two/coredns":  {Root: "/h2/coredns"},
			"aws/api":              {Root: "/aws/api"},
		},
		DiscoveredStacks: map[string]manifest.Stack{
			"aws/worker": {Root: "/aws/worker"},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{
			"h1-traefik-config": {Context: "hetzner-one", Stack: "traefik", Source: "/h1/traefik/volumes/config"},
			"aws-api-data":      {Context: "aws", Stack: "api", Source: "/aws/api/volumes/data"},
		},
		Deployments: map[string]manifest.DeploymentConfig{
			"core-infra": {
				Stacks: []string{"hetzner-two/traefik", "hetzner-two/coredns"},
			},
			"all-hetzner": {
				Contexts: []string{"hetzner-one", "hetzner-two"},
			},
		},
	}
}

func TestResolveTargets_Empty(t *testing.T) {
	cfg := multiContextConfig()
	got, err := ResolveTargets(cfg, TargetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != cfg {
		t.Fatal("expected same pointer when no targeting")
	}
}

func TestResolveTargets_ContextFilter(t *testing.T) {
	cfg := multiContextConfig()
	got, err := ResolveTargets(cfg, TargetOptions{Contexts: []string{"aws"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Contexts) != 1 {
		t.Fatalf("expected 1 context, got %d", len(got.Contexts))
	}
	if _, ok := got.Contexts["aws"]; !ok {
		t.Fatal("expected aws context")
	}
	if len(got.Stacks) != 1 || got.Stacks["aws/api"].Root != "/aws/api" {
		t.Fatalf("expected aws/api stack, got %v", got.Stacks)
	}
	if len(got.DiscoveredStacks) != 1 {
		t.Fatalf("expected 1 discovered stack, got %d", len(got.DiscoveredStacks))
	}
	if len(got.DiscoveredFilesets) != 1 {
		t.Fatalf("expected 1 fileset, got %d", len(got.DiscoveredFilesets))
	}
}

func TestResolveTargets_StackFilter(t *testing.T) {
	cfg := multiContextConfig()
	got, err := ResolveTargets(cfg, TargetOptions{Stacks: []string{"hetzner-one/traefik"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Contexts) != 1 {
		t.Fatalf("expected 1 context, got %d", len(got.Contexts))
	}
	if len(got.Stacks) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(got.Stacks))
	}
	if _, ok := got.Stacks["hetzner-one/traefik"]; !ok {
		t.Fatal("expected hetzner-one/traefik")
	}
	// Filesets for hetzner-one/traefik should be included
	if len(got.DiscoveredFilesets) != 1 {
		t.Fatalf("expected 1 fileset, got %d", len(got.DiscoveredFilesets))
	}
}

func TestResolveTargets_Deployment(t *testing.T) {
	cfg := multiContextConfig()
	got, err := ResolveTargets(cfg, TargetOptions{Deployment: "core-infra"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Contexts) != 1 {
		t.Fatalf("expected 1 context, got %d", len(got.Contexts))
	}
	if _, ok := got.Contexts["hetzner-two"]; !ok {
		t.Fatal("expected hetzner-two context")
	}
	if len(got.Stacks) != 2 {
		t.Fatalf("expected 2 stacks, got %d", len(got.Stacks))
	}
}

func TestResolveTargets_DeploymentWithContexts(t *testing.T) {
	cfg := multiContextConfig()
	got, err := ResolveTargets(cfg, TargetOptions{Deployment: "all-hetzner"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %d", len(got.Contexts))
	}
	// All stacks from hetzner-one and hetzner-two
	if len(got.Stacks) != 4 {
		t.Fatalf("expected 4 stacks, got %d", len(got.Stacks))
	}
}

func TestResolveTargets_UnknownContext(t *testing.T) {
	cfg := multiContextConfig()
	_, err := ResolveTargets(cfg, TargetOptions{Contexts: []string{"nope"}})
	if err == nil {
		t.Fatal("expected error for unknown context")
	}
}

func TestResolveTargets_UnknownDeployment(t *testing.T) {
	cfg := multiContextConfig()
	_, err := ResolveTargets(cfg, TargetOptions{Deployment: "nope"})
	if err == nil {
		t.Fatal("expected error for unknown deployment")
	}
}

func TestResolveTargets_InvalidStackFormat(t *testing.T) {
	cfg := multiContextConfig()
	_, err := ResolveTargets(cfg, TargetOptions{Stacks: []string{"no-slash"}})
	if err == nil {
		t.Fatal("expected error for invalid stack format")
	}
}

func TestResolveTargets_StackReferencesUnknownContext(t *testing.T) {
	cfg := multiContextConfig()
	_, err := ResolveTargets(cfg, TargetOptions{Stacks: []string{"unknown/app"}})
	if err == nil {
		t.Fatal("expected error for unknown context in stack")
	}
}

func TestResolveTargets_OriginalUnmodified(t *testing.T) {
	cfg := multiContextConfig()
	originalContexts := len(cfg.Contexts)
	originalStacks := len(cfg.Stacks)

	_, err := ResolveTargets(cfg, TargetOptions{Contexts: []string{"aws"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Contexts) != originalContexts {
		t.Fatal("original config contexts modified")
	}
	if len(cfg.Stacks) != originalStacks {
		t.Fatal("original config stacks modified")
	}
}
