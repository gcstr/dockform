package imagescmd

import (
	"context"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/planner"
)

// composeClient is the subset of *dockercli.Client that imagescmd needs.
// Declaring it here keeps the compose calls injectable in tests.
type composeClient interface {
	ComposeConfigFull(ctx context.Context, workingDir string, files, profiles, envFiles []string, inlineEnv []string) (dockercli.ComposeConfigDoc, error)
	ComposePull(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, services []string, inlineEnv []string) (string, error)
	ComposeUp(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, inlineEnv []string) (string, error)
}

// clientGetter resolves the compose client for a docker context name.
type clientGetter func(ctxName string) composeClient

// factoryClientGetter adapts the real client factory to a clientGetter.
func factoryClientGetter(factory *dockercli.DefaultClientFactory, cfg *manifest.Config) clientGetter {
	return func(ctxName string) composeClient {
		return factory.GetClientForContext(ctxName, cfg)
	}
}

// stackInlineEnv builds the inline environment for a stack, including its
// SOPS-decrypted secrets. This is the same environment the apply path builds,
// so image commands recreate containers with identical env to `dockform apply`.
func stackInlineEnv(ctx context.Context, stack manifest.Stack, cfg *manifest.Config) ([]string, error) {
	var sops *manifest.SopsConfig
	if cfg != nil {
		sops = cfg.Sops
	}
	return planner.NewServiceStateDetector(nil).BuildInlineEnv(ctx, stack, sops)
}
