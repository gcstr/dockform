package planner

import (
	"context"

	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
)

// BuildPlan produces a structured plan with resources organized by context and type.
// For multi-context configs, it builds per-context plans and aggregates them.
func (p *Planner) BuildPlan(ctx context.Context, cfg manifest.Config) (*Plan, error) {
	log := logger.FromContext(ctx).With("component", "planner")

	// Get all stacks (discovered + explicit)
	allStacks := cfg.GetAllStacks()
	allFilesets := cfg.GetAllFilesets()

	st := logger.StartStep(log, "plan_build", "multi-context",
		"resource_kind", "plan",
		"contexts", len(cfg.Contexts),
		"stacks_desired", len(allStacks),
		"filesets_desired", len(allFilesets))

	// Initialize multi-context execution context
	multiExecCtx := NewMultiContextExecutionContext()

	// Aggregated resource plan (combines all contexts for display)
	aggregatedPlan := &ResourcePlan{
		Volumes:    []Resource{},
		Networks:   []Resource{},
		Stacks:     make(map[string][]Resource),
		Filesets:   make(map[string][]Resource),
		Containers: []Resource{},
	}

	// Per-context plans
	byDaemon := make(map[string]*ContextPlan)

	// Process each daemon
	contextNames := sortedKeys(cfg.Contexts)
	for _, contextName := range contextNames {
		contextConfig := cfg.Contexts[contextName]

		// Get Docker client for this context
		client := p.getClientForContext(contextName, &cfg)

		// Initialize context execution context
		contextExecCtx := NewContextExecutionContext(contextName, cfg.Identifier)

		// Build plan for this context
		contextPlan, err := p.buildContextPlan(ctx, cfg, contextName, contextConfig, client, contextExecCtx)
		if err != nil {
			return nil, err
		}

		byDaemon[contextName] = contextPlan
		multiExecCtx.ByContext[contextName] = contextExecCtx

		// Aggregate into combined plan
		p.aggregateContextPlan(aggregatedPlan, contextPlan)
	}

	// Check if we have any resources
	hasResources := len(aggregatedPlan.Volumes) > 0 || len(aggregatedPlan.Networks) > 0 ||
		len(aggregatedPlan.Stacks) > 0 || len(aggregatedPlan.Filesets) > 0 ||
		len(aggregatedPlan.Containers) > 0

	if !hasResources {
		// Add a special "nothing to do" resource
		aggregatedPlan.Volumes = append(aggregatedPlan.Volumes,
			NewResource(ResourceVolume, "nothing to do", ActionNoop, "nothing to do"))
	}

	// Calculate plan statistics for logging
	createCount, updateCount, deleteCount := aggregatedPlan.CountActions()
	totalChanges := createCount + updateCount + deleteCount

	// Log completion with plan summary
	st.OK(totalChanges > 0,
		"changes_create", createCount,
		"changes_update", updateCount,
		"changes_delete", deleteCount,
		"changes_total", totalChanges)

	return &Plan{
		ByContext:        byDaemon,
		Resources:        aggregatedPlan,
		ExecutionContext: multiExecCtx,
	}, nil
}

// buildContextPlan builds a plan for a single contextConfig.
func (p *Planner) buildContextPlan(ctx context.Context, cfg manifest.Config, contextName string, contextConfig manifest.ContextConfig, client DockerClient, execCtx *ContextExecutionContext) (*ContextPlan, error) {
	log := logger.FromContext(ctx).With("component", "planner", "context", contextName)

	resourcePlan := &ResourcePlan{
		Volumes:    []Resource{},
		Networks:   []Resource{},
		Stacks:     make(map[string][]Resource),
		Filesets:   make(map[string][]Resource),
		Containers: []Resource{},
	}

	// Get stacks and filesets for this context
	contextStacks := cfg.GetStacksForContext(contextName)
	contextFilesets := cfg.GetFilesetsForContext(contextName)

	// Accumulate existing sets when docker client is available
	var existingVolumes, existingNetworks map[string]struct{}
	if client != nil {
		var err error
		existingVolumes, existingNetworks, err = p.getExistingResourcesForClient(ctx, client)
		if err != nil {
			return nil, err
		}
		// Store in execution context for reuse during apply
		execCtx.ExistingVolumes = existingVolumes
		execCtx.ExistingNetworks = existingNetworks
		log.Debug("resource_discovery",
			"context", contextName,
			"volumes_found", len(existingVolumes),
			"networks_found", len(existingNetworks))
	}

	// Plan volumes - combine volumes from filesets + explicit context volumes
	desiredVolumes := map[string]struct{}{}
	for _, fileset := range contextFilesets {
		desiredVolumes[fileset.TargetVolume] = struct{}{}
	}
	// Add explicit volumes declared in context config
	for volName := range contextConfig.Volumes {
		desiredVolumes[volName] = struct{}{}
	}

	volNames := sortedKeys(desiredVolumes)
	for _, name := range volNames {
		exists := false
		if existingVolumes != nil {
			_, exists = existingVolumes[name]
		}
		if exists {
			resourcePlan.Volumes = append(resourcePlan.Volumes,
				NewResource(ResourceVolume, name, ActionNoop, "exists"))
		} else {
			resourcePlan.Volumes = append(resourcePlan.Volumes,
				NewResource(ResourceVolume, name, ActionCreate, ""))
		}
	}
	// Plan removals for labeled volumes no longer needed (skip when targeting specific stacks)
	if !cfg.Targeted {
		for name := range existingVolumes {
			if _, want := desiredVolumes[name]; !want {
				resourcePlan.Volumes = append(resourcePlan.Volumes,
					NewResource(ResourceVolume, name, ActionDelete, ""))
			}
		}
	}

	// Plan networks - combine explicit context networks + track existing ones
	desiredNetworks := map[string]struct{}{}
	for netName := range contextConfig.Networks {
		desiredNetworks[netName] = struct{}{}
	}

	netNames := sortedKeys(desiredNetworks)
	for _, name := range netNames {
		exists := false
		if existingNetworks != nil {
			_, exists = existingNetworks[name]
		}
		if exists {
			resourcePlan.Networks = append(resourcePlan.Networks,
				NewResource(ResourceNetwork, name, ActionNoop, "exists"))
		} else {
			resourcePlan.Networks = append(resourcePlan.Networks,
				NewResource(ResourceNetwork, name, ActionCreate, ""))
		}
	}
	// Plan removals for labeled networks no longer needed (skip when targeting specific stacks)
	if !cfg.Targeted {
		for name := range existingNetworks {
			if _, want := desiredNetworks[name]; !want {
				resourcePlan.Networks = append(resourcePlan.Networks,
					NewResource(ResourceNetwork, name, ActionDelete, ""))
			}
		}
	}

	// Build stack resources
	if err := p.buildStackResourcesForContext(ctx, cfg, contextName, contextStacks, cfg.Identifier, client, resourcePlan, execCtx); err != nil {
		return nil, err
	}

	// Track services that should be removed (orphan detection)
	// Skip when targeting specific stacks — we only have a partial view of desired state
	if client != nil && !cfg.Targeted {
		desiredServices, err := p.collectDesiredServicesForContext(ctx, cfg, contextStacks, client)
		if err != nil {
			return nil, err
		}
		if all, err := client.ListComposeContainersAll(ctx); err == nil {
			toDelete := map[string]map[string]struct{}{}
			for _, it := range all {
				if _, want := desiredServices[it.Service]; !want {
					if toDelete[it.Project] == nil {
						toDelete[it.Project] = map[string]struct{}{}
					}
					toDelete[it.Project][it.Service] = struct{}{}
				}
			}
			// Add deletions under Stacks section
			for stackName, services := range toDelete {
				for svc := range services {
					resourcePlan.Stacks[stackName] = append(resourcePlan.Stacks[stackName],
						NewResource(ResourceService, svc, ActionDelete, ""))
				}
			}
		}
	}

	// Filesets: show per-file changes using remote index when available
	if client != nil && len(contextFilesets) > 0 {
		if err := p.buildFilesetResourcesForContext(ctx, contextFilesets, existingVolumes, client, resourcePlan, execCtx); err != nil {
			return nil, err
		}
	}

	return &ContextPlan{
		ContextName: contextName,
		Identifier:  cfg.Identifier,
		Resources:   resourcePlan,
	}, nil
}
