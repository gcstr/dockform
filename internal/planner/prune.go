package planner

import (
	"context"
	"path/filepath"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/secrets"
)

// Prune removes unmanaged resources labeled with the identifier.
// It deletes volumes, networks, and containers that are labeled but not present in cfg.
func (p *Planner) Prune(ctx context.Context, cfg manifest.Config) error {
	return p.PruneWithPlan(ctx, cfg, nil)
}

// PruneWithPlan removes unmanaged resources, optionally reusing execution context from a pre-built plan.
func (p *Planner) PruneWithPlan(ctx context.Context, cfg manifest.Config, plan *Plan) error {
	if p.docker == nil && p.factory == nil {
		return apperr.New("planner.Prune", apperr.Precondition, "docker client not configured")
	}

	return p.ExecuteAcrossContexts(ctx, &cfg, func(ctx context.Context, contextName string) error {
		client := p.getClientForContext(contextName, &cfg)
		if client == nil {
			return apperr.New("planner.Prune", apperr.Precondition, "docker client not available for context %s", contextName)
		}

		return p.pruneContext(ctx, cfg, contextName, client, plan)
	})
}

// pruneContext removes unmanaged resources for a single context.
func (p *Planner) pruneContext(ctx context.Context, cfg manifest.Config, contextName string, client DockerClient, plan *Plan) error {
	contextStacks := cfg.GetStacksForContext(contextName)
	contextFilesets := cfg.GetFilesetsForContext(contextName)

	// Desired services set for this context
	desiredServices := map[string]struct{}{}

	if plan != nil && plan.ExecutionContext != nil {
		if contextCtx := plan.ExecutionContext.ByContext[contextName]; contextCtx != nil {
			for stackName, stack := range contextStacks {
				if execData := contextCtx.Stacks[stackName]; execData != nil && execData.Services != nil {
					for _, svc := range execData.Services {
						desiredServices[svc.Name] = struct{}{}
					}
				} else {
					collectDesiredServicesForStack(ctx, client, stack, cfg.Sops, desiredServices)
				}
			}
		} else {
			for _, stack := range contextStacks {
				collectDesiredServicesForStack(ctx, client, stack, cfg.Sops, desiredServices)
			}
		}
	} else {
		for _, stack := range contextStacks {
			collectDesiredServicesForStack(ctx, client, stack, cfg.Sops, desiredServices)
		}
	}

	// Remove labeled containers not in desired set
	if all, err := client.ListComposeContainersAll(ctx); err == nil {
		for _, it := range all {
			if _, want := desiredServices[it.Service]; !want {
				_ = client.RemoveContainer(ctx, it.Name, true)
			}
		}
	}

	// Remove labeled volumes not needed by any fileset or explicit context config
	desiredVolumes := map[string]struct{}{}
	for _, fileset := range contextFilesets {
		desiredVolumes[fileset.TargetVolume] = struct{}{}
	}
	// Add explicit volumes from context config
	if contextConfig, ok := cfg.Contexts[contextName]; ok {
		for volName := range contextConfig.Volumes {
			desiredVolumes[volName] = struct{}{}
		}
	}
	if vols, err := client.ListVolumes(ctx); err == nil {
		for _, v := range vols {
			if _, want := desiredVolumes[v]; !want {
				_ = client.RemoveVolume(ctx, v)
			}
		}
	}

	// Remove labeled networks not defined in context config
	desiredNetworks := map[string]struct{}{}
	if contextConfig, ok := cfg.Contexts[contextName]; ok {
		for netName := range contextConfig.Networks {
			desiredNetworks[netName] = struct{}{}
		}
	}
	if nets, err := client.ListNetworks(ctx); err == nil {
		for _, n := range nets {
			if _, want := desiredNetworks[n]; !want {
				_ = client.RemoveNetwork(ctx, n)
			}
		}
	}

	return nil
}

// collectDesiredServicesForStack collects service names for a single stack by querying compose config.
func collectDesiredServicesForStack(ctx context.Context, client DockerClient, stack manifest.Stack, sopsConfig *manifest.SopsConfig, desiredServices map[string]struct{}) {
	inline := append([]string(nil), stack.EnvInline...)
	ageKeyFile := ""
	pgpDir := ""
	pgpAgent := false
	pgpMode := ""
	pgpPass := ""
	if sopsConfig != nil && sopsConfig.Age != nil {
		ageKeyFile = sopsConfig.Age.KeyFile
	}
	if sopsConfig != nil && sopsConfig.Pgp != nil {
		pgpDir = sopsConfig.Pgp.KeyringDir
		pgpAgent = sopsConfig.Pgp.UseAgent
		pgpMode = sopsConfig.Pgp.PinentryMode
		pgpPass = sopsConfig.Pgp.Passphrase
	}
	for _, pth0 := range stack.SopsSecrets {
		pth := pth0
		if pth != "" && !filepath.IsAbs(pth) {
			pth = filepath.Join(stack.Root, pth)
		}
		if pairs, err := secrets.DecryptAndParse(ctx, pth, secrets.SopsOptions{AgeKeyFile: ageKeyFile, PgpKeyringDir: pgpDir, PgpUseAgent: pgpAgent, PgpPinentryMode: pgpMode, PgpPassphrase: pgpPass}); err == nil {
			inline = append(inline, pairs...)
		}
	}
	if doc, err := client.ComposeConfigFull(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, inline); err == nil {
		for s := range doc.Services {
			desiredServices[s] = struct{}{}
		}
	}
}
