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
	// Skip pruning when targeting specific stacks — we only have a partial view of desired state
	if cfg.Targeted {
		return nil
	}

	if p.docker == nil && p.factory == nil {
		return apperr.New("planner.Prune", apperr.Precondition, "docker client not configured")
	}

	// Prune mutates state (removes containers/networks/volumes), so contexts
	// always run to completion: a failure on one host must never cancel
	// in-flight cleanup work on another host.
	err := p.ExecuteAcrossContextsMode(ctx, &cfg, RunToCompletion, func(ctx context.Context, contextName string) error {
		client := p.getClientForContext(contextName, &cfg)
		if client == nil {
			return apperr.New("planner.Prune", apperr.Precondition, "docker client not available for context %s", contextName)
		}

		return p.pruneContext(ctx, cfg, contextName, client, plan)
	})
	return handleCleanupError(ctx, err, opts, "prune")
}

// pruneContext removes unmanaged resources for a single context.
func (p *Planner) pruneContext(ctx context.Context, cfg manifest.Config, contextName string, client DockerClient, plan *Plan) error {
	// Resources the plan marked for deletion were seeded as pending lines in the
	// apply view, but apply never removes them — prune does. Without reporting
	// here they stayed pending forever: their group never collapsed, the footer
	// never reached its total, and the plain summary called an approved, completed
	// deletion "not applied".
	progress := orNop(p.reporter)
	deletes := plannedDeletes(plan, contextName)

	contextStacks := cfg.GetStacksForContext(contextName)
	contextFilesets := cfg.GetFilesetsForContext(contextName)

	// What should be running on this context, grouped by compose project. If a
	// stack's project cannot be resolved, prune keeps every compose-owned network
	// and matches containers on service name alone, rather than risk removing an
	// active stack's resources.
	desired := newDesiredStacks()
	var errs []error
	canPruneContainers := true

	var contextCtx *ContextExecutionContext
	if plan != nil && plan.ExecutionContext != nil {
		contextCtx = plan.ExecutionContext.ByContext[contextName]
	}
	for stackName, stack := range contextStacks {
		var names, inline []string
		if execData := stackExecData(contextCtx, stackName); execData != nil && execData.Services != nil {
			for _, svc := range execData.Services {
				names = append(names, svc.Name)
			}
			inline = execData.InlineEnv
		} else {
			n, in, err := desiredServicesForStack(ctx, client, stack, cfg.Sops)
			if err != nil {
				canPruneContainers = false
				errs = append(errs, err)
				desired.add("", nil)
				continue
			}
			names, inline = n, in
		}
		project, err := stackComposeProject(ctx, client, stack, inline)
		if err != nil {
			logger.FromContext(ctx).Warn("compose_project_unresolved", "context", contextName, "stack", stackName, "error", err)
			project = ""
		}
		desired.add(project, names)
	}

	// Remove labeled containers not in desired set
	if canPruneContainers {
		all, err := client.ListComposeContainersAll(ctx)
		if err != nil {
			errs = append(errs, apperr.Wrap("planner.pruneContext", apperr.External, err, "list managed containers for context %s", contextName))
		} else {
			for _, it := range all {
				if !desired.wantsContainer(it.Project, it.Service) {
					// An orphan service is seeded by service name; a plan that
					// enumerated the container itself is keyed by container name.
					k := deleteKey{Type: ResourceService, Name: it.Service}
					if _, seeded := deletes[k]; !seeded {
						k = deleteKey{Type: ResourceContainer, Name: it.Name}
					}
					if err := reportRemoval(progress, deletes, k, func() error {
						return client.RemoveContainer(ctx, it.Name, true)
					}); err != nil {
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
				if err := reportRemoval(progress, deletes, deleteKey{Type: ResourceVolume, Name: v}, func() error {
					return client.RemoveVolume(ctx, v)
				}); err != nil {
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
		// Compose-owned networks carry the identifier label but are managed by
		// their stack's lifecycle, so they are only pruned once that stack's
		// project is gone (GH #54).
		composeOwned, err := p.getComposeOwnedNetworks(ctx, client)
		if err != nil {
			errs = append(errs, err)
		}
		existing := make(map[string]struct{}, len(nets))
		for _, n := range nets {
			existing[n] = struct{}{}
		}
		for _, n := range orphanNetworks(existing, desiredNetworks, composeOwned, desired.projects()) {
			if err := reportRemoval(progress, deletes, deleteKey{Type: ResourceNetwork, Name: n}, func() error {
				return client.RemoveNetwork(ctx, n)
			}); err != nil {
				errs = append(errs, apperr.Wrap("planner.pruneContext", apperr.External, err, "remove unmanaged network %s in context %s", n, contextName))
			}
		}
	}

	return apperr.Aggregate("planner.pruneContext", apperr.External, fmt.Sprintf("prune for context %s failed for one or more resources", contextName), errs...)
}

// stackExecData returns the plan's pre-computed data for a stack, or nil.
func stackExecData(contextCtx *ContextExecutionContext, stackName string) *StackExecutionData {
	if contextCtx == nil {
		return nil
	}
	return contextCtx.Stacks[stackName]
}

// desiredServicesForStack lists a single stack's service names by querying
// compose config, and returns the inline environment it built for that.
func desiredServicesForStack(ctx context.Context, client DockerClient, stack manifest.Stack, sopsConfig *manifest.SopsConfig) ([]string, []string, error) {
	detector := NewServiceStateDetector(client)
	inline, err := detector.BuildInlineEnv(ctx, stack, sopsConfig)
	if err != nil {
		return nil, nil, apperr.Wrap("planner.desiredServicesForStack", apperr.External, err, "build inline env for stack %s", stack.Root)
	}
	names, err := detector.GetPlannedServices(ctx, stack, inline)
	if err != nil {
		return nil, nil, apperr.Wrap("planner.desiredServicesForStack", apperr.External, err, "list planned services for stack %s", stack.Root)
	}
	return names, inline, nil
}
