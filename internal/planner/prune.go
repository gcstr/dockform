package planner

import (
	"context"
	"fmt"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
)

// Prune removes unmanaged resources labeled with the identifier.
// It deletes volumes, networks, and containers that are labeled but not present in cfg.
func (p *Planner) Prune(ctx context.Context, cfg manifest.Config) error {
	return p.PruneWithPlanOptions(ctx, cfg, nil, CleanupOptions{Strict: true, VerboseErrors: true})
}

// PruneWithPlan removes unmanaged resources, optionally reusing execution context from a pre-built plan.
func (p *Planner) PruneWithPlan(ctx context.Context, cfg manifest.Config, plan *Plan) error {
	return p.PruneWithPlanOptions(ctx, cfg, plan, CleanupOptions{Strict: true, VerboseErrors: true})
}

// PruneWithPlanOptions removes unmanaged resources using explicit cleanup behavior options.
func (p *Planner) PruneWithPlanOptions(ctx context.Context, cfg manifest.Config, plan *Plan, opts CleanupOptions) error {
	if p.docker == nil && p.factory == nil {
		return apperr.New("planner.Prune", apperr.Precondition, "docker client not configured")
	}

	err := p.ExecuteAcrossContexts(ctx, &cfg, func(ctx context.Context, contextName string) error {
		client := p.getClientForContext(contextName, &cfg)
		if client == nil {
			return apperr.New("planner.Prune", apperr.Precondition, "docker client not available for context %s", contextName)
		}

		return p.pruneContext(ctx, cfg, contextName, client, plan)
	})
	if err == nil {
		return nil
	}
	if opts.Strict {
		return err
	}

	log := logger.FromContext(ctx).With("component", "planner", "action", "prune")
	if opts.VerboseErrors {
		log.Warn("prune_non_strict_errors", "error", err.Error())
	} else {
		log.Warn("prune_non_strict_errors")
	}
	return nil
}

// pruneContext removes unmanaged resources for a single context.
func (p *Planner) pruneContext(ctx context.Context, cfg manifest.Config, contextName string, client DockerClient, plan *Plan) error {
	contextStacks := cfg.GetStacksForContext(contextName)
	contextFilesets := cfg.GetFilesetsForContext(contextName)

	// Desired services set for this context
	desiredServices := map[string]struct{}{}
	var errs []error
	canPruneContainers := true

	if plan != nil && plan.ExecutionContext != nil {
		if contextCtx := plan.ExecutionContext.ByContext[contextName]; contextCtx != nil {
			for stackName, stack := range contextStacks {
				if execData := contextCtx.Stacks[stackName]; execData != nil && execData.Services != nil {
					for _, svc := range execData.Services {
						desiredServices[svc.Name] = struct{}{}
					}
				} else {
					if err := collectDesiredServicesForStack(ctx, client, stack, cfg.Sops, desiredServices); err != nil {
						canPruneContainers = false
						errs = append(errs, err)
					}
				}
			}
		} else {
			for _, stack := range contextStacks {
				if err := collectDesiredServicesForStack(ctx, client, stack, cfg.Sops, desiredServices); err != nil {
					canPruneContainers = false
					errs = append(errs, err)
				}
			}
		}
	} else {
		for _, stack := range contextStacks {
			if err := collectDesiredServicesForStack(ctx, client, stack, cfg.Sops, desiredServices); err != nil {
				canPruneContainers = false
				errs = append(errs, err)
			}
		}
	}

	// Remove labeled containers not in desired set
	if canPruneContainers {
		all, err := client.ListComposeContainersAll(ctx)
		if err != nil {
			errs = append(errs, apperr.Wrap("planner.pruneContext", apperr.External, err, "list managed containers for context %s", contextName))
		} else {
			for _, it := range all {
				if _, want := desiredServices[it.Service]; !want {
					if err := client.RemoveContainer(ctx, it.Name, true); err != nil {
						errs = append(errs, apperr.Wrap("planner.pruneContext", apperr.External, err, "remove unmanaged container %s in context %s", it.Name, contextName))
					}
				}
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
	vols, err := client.ListVolumes(ctx)
	if err != nil {
		errs = append(errs, apperr.Wrap("planner.pruneContext", apperr.External, err, "list managed volumes for context %s", contextName))
	} else {
		for _, v := range vols {
			if _, want := desiredVolumes[v]; !want {
				if err := client.RemoveVolume(ctx, v); err != nil {
					errs = append(errs, apperr.Wrap("planner.pruneContext", apperr.External, err, "remove unmanaged volume %s in context %s", v, contextName))
				}
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
	nets, err := client.ListNetworks(ctx)
	if err != nil {
		errs = append(errs, apperr.Wrap("planner.pruneContext", apperr.External, err, "list managed networks for context %s", contextName))
	} else {
		for _, n := range nets {
			if _, want := desiredNetworks[n]; !want {
				if err := client.RemoveNetwork(ctx, n); err != nil {
					errs = append(errs, apperr.Wrap("planner.pruneContext", apperr.External, err, "remove unmanaged network %s in context %s", n, contextName))
				}
			}
		}
	}

	return apperr.Aggregate("planner.pruneContext", apperr.External, fmt.Sprintf("prune for context %s failed for one or more resources", contextName), errs...)
}

// collectDesiredServicesForStack collects service names for a single stack by querying compose config.
func collectDesiredServicesForStack(ctx context.Context, client DockerClient, stack manifest.Stack, sopsConfig *manifest.SopsConfig, desiredServices map[string]struct{}) error {
	detector := NewServiceStateDetector(client)
	inline, err := detector.BuildInlineEnv(ctx, stack, sopsConfig)
	if err != nil {
		return apperr.Wrap("planner.collectDesiredServicesForStack", apperr.External, err, "build inline env for stack %s", stack.Root)
	}
	names, err := detector.GetPlannedServices(ctx, stack, inline)
	if err != nil {
		return apperr.Wrap("planner.collectDesiredServicesForStack", apperr.External, err, "list planned services for stack %s", stack.Root)
	}
	for _, name := range names {
		desiredServices[name] = struct{}{}
	}
	return nil
}
