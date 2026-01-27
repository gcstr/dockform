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
	if p.docker == nil {
		return apperr.New("planner.Prune", apperr.Precondition, "docker client not configured")
	}

	// Desired services set across all stacks
	desiredServices := map[string]struct{}{}

	// Get all stacks (discovered + explicit)
	allStacks := cfg.GetAllStacks()

	// If we have execution context, use the pre-computed service lists where available
	if plan != nil && plan.ExecutionContext != nil {
		// Iterate over all daemons and their stacks
		for daemonName, daemonCtx := range plan.ExecutionContext.ByDaemon {
			daemonStacks := cfg.GetStacksForDaemon(daemonName)
			for stackName, stack := range daemonStacks {
				if execData := daemonCtx.Stacks[stackName]; execData != nil && execData.Services != nil {
					// Use cached service list from execution context
					for _, svc := range execData.Services {
						desiredServices[svc.Name] = struct{}{}
					}
				} else {
					// Fallback: collect fresh for stacks missing from execution context
					p.collectDesiredServicesForStack(ctx, stack, cfg.Sops, desiredServices)
				}
			}
		}
	} else {
		// Fallback: detect services fresh (original behavior)
		for _, stack := range allStacks {
			p.collectDesiredServicesForStack(ctx, stack, cfg.Sops, desiredServices)
		}
	}

	// Remove labeled containers not in desired set
	if all, err := p.docker.ListComposeContainersAll(ctx); err == nil {
		for _, it := range all {
			if _, want := desiredServices[it.Service]; !want {
				_ = p.docker.RemoveContainer(ctx, it.Name, true)
			}
		}
	}

	// Remove labeled volumes not needed by any fileset
	allFilesets := cfg.GetAllFilesets()
	desiredVolumes := map[string]struct{}{}
	for _, fileset := range allFilesets {
		desiredVolumes[fileset.TargetVolume] = struct{}{}
	}
	if vols, err := p.docker.ListVolumes(ctx); err == nil {
		for _, v := range vols {
			if _, want := desiredVolumes[v]; !want {
				_ = p.docker.RemoveVolume(ctx, v)
			}
		}
	}

	// In the new schema, networks are managed by compose - we don't prune them explicitly
	// They will be cleaned up when compose down is run

	return nil
}

// collectDesiredServicesForStack collects service names for a single stack by querying compose config.
// This is extracted as a helper to avoid code duplication.
func (p *Planner) collectDesiredServicesForStack(ctx context.Context, stack manifest.Stack, sopsConfig *manifest.SopsConfig, desiredServices map[string]struct{}) {
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
	if doc, err := p.docker.ComposeConfigFull(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, inline); err == nil {
		for s := range doc.Services {
			desiredServices[s] = struct{}{}
		}
	}
}
