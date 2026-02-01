package planner

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/gcstr/dockform/internal/filesets"
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
		ByContext:         byDaemon,
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
		existingVolumes, existingNetworks = p.getExistingResourcesForClient(ctx, client)
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
	// Plan removals for labeled volumes no longer needed
	for name := range existingVolumes {
		if _, want := desiredVolumes[name]; !want {
			resourcePlan.Volumes = append(resourcePlan.Volumes,
				NewResource(ResourceVolume, name, ActionDelete, ""))
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
	// Plan removals for labeled networks no longer needed
	for name := range existingNetworks {
		if _, want := desiredNetworks[name]; !want {
			resourcePlan.Networks = append(resourcePlan.Networks,
				NewResource(ResourceNetwork, name, ActionDelete, ""))
		}
	}

	// Build stack resources
	if err := p.buildStackResourcesForContext(ctx, cfg, contextName, contextStacks, cfg.Identifier, client, resourcePlan, execCtx); err != nil {
		return nil, err
	}

	// Track services that should be removed (orphan detection)
	if client != nil {
		desiredServices := p.collectDesiredServicesForContext(ctx, cfg, contextStacks, client)
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
		p.buildFilesetResourcesForContext(ctx, contextFilesets, existingVolumes, client, resourcePlan, execCtx)
	}

	return &ContextPlan{
		ContextName: contextName,
		Identifier:  cfg.Identifier,
		Resources:   resourcePlan,
	}, nil
}

// buildStackResourcesForContext analyzes stacks for a context and adds service resources to the plan.
func (p *Planner) buildStackResourcesForContext(ctx context.Context, cfg manifest.Config, contextName string, stacks map[string]manifest.Stack, identifier string, client DockerClient, plan *ResourcePlan, execCtx *ContextExecutionContext) error {
	if len(stacks) == 0 {
		return nil
	}

	if client == nil {
		// Without Docker client, we can only show planned stacks
		for stackName := range stacks {
			plan.Stacks[stackName] = []Resource{
				NewResource(ResourceService, "services", ActionNoop, "planned (services diff TBD)"),
			}
		}
		return nil
	}

	// Choose parallel or sequential processing based on configuration
	if p.parallel {
		return p.buildStackResourcesParallelForContext(ctx, cfg, contextName, stacks, identifier, client, plan, execCtx)
	}
	return p.buildStackResourcesSequentialForContext(ctx, cfg, contextName, stacks, identifier, client, plan, execCtx)
}

// serviceStatesToResources converts service states to plan resources.
// This is the core conversion logic used by both sequential and parallel stack processing.
func serviceStatesToResources(services []ServiceInfo) []Resource {
	var resources []Resource
	for _, service := range services {
		switch service.State {
		case ServiceMissing:
			resources = append(resources,
				NewResource(ResourceService, service.Name, ActionCreate, ""))
		case ServiceIdentifierMismatch:
			resources = append(resources,
				NewResource(ResourceService, service.Name, ActionReconcile, "identifier mismatch"))
		case ServiceDrifted:
			resources = append(resources,
				NewResource(ResourceService, service.Name, ActionUpdate, "config drift"))
		case ServiceRunning:
			if service.DesiredHash != "" {
				resources = append(resources,
					NewResource(ResourceService, service.Name, ActionNoop, "up-to-date"))
			} else {
				// Fallback when hash is unavailable
				resources = append(resources,
					NewResource(ResourceService, service.Name, ActionNoop, "running"))
			}
		}
	}
	return resources
}

// fallbackStackResource returns a placeholder resource when stack analysis fails.
func fallbackStackResource() []Resource {
	return []Resource{
		NewResource(ResourceService, "services", ActionNoop, "planned (services diff TBD)"),
	}
}

// buildStackResourcesSequentialForContext processes stacks one by one for a context
func (p *Planner) buildStackResourcesSequentialForContext(ctx context.Context, cfg manifest.Config, contextName string, stacks map[string]manifest.Stack, identifier string, client DockerClient, plan *ResourcePlan, execCtx *ContextExecutionContext) error {
	detector := NewServiceStateDetector(client)

	// Process stacks in sorted order for deterministic output
	stackNames := make([]string, 0, len(stacks))
	for name := range stacks {
		stackNames = append(stackNames, name)
	}
	sort.Strings(stackNames)

	for _, stackName := range stackNames {
		stack := stacks[stackName]

		// Build inline environment (including decrypted secrets)
		inline := detector.BuildInlineEnv(ctx, stack, cfg.Sops)

		services, err := detector.DetectAllServicesState(ctx, stackName, stack, identifier, cfg.Sops)
		if err != nil || len(services) == 0 {
			plan.Stacks[stackName] = fallbackStackResource()
			continue
		}

		// Store execution data for reuse during apply
		execCtx.Stacks[stackName] = &StackExecutionData{
			Services:   services,
			InlineEnv:  inline,
			NeedsApply: NeedsApply(services),
		}

		plan.Stacks[stackName] = serviceStatesToResources(services)
	}

	return nil
}

// buildStackResourcesParallelForContext processes stacks concurrently for a context
func (p *Planner) buildStackResourcesParallelForContext(ctx context.Context, cfg manifest.Config, contextName string, stacks map[string]manifest.Stack, identifier string, client DockerClient, plan *ResourcePlan, execCtx *ContextExecutionContext) error {
	detector := NewServiceStateDetector(client).WithParallel(true)

	// Sort stack names for deterministic processing
	stackNames := make([]string, 0, len(stacks))
	for name := range stacks {
		stackNames = append(stackNames, name)
	}
	sort.Strings(stackNames)

	type stackResult struct {
		stackName string
		resources []Resource
		execData  *StackExecutionData
	}

	resultsChan := make(chan stackResult, len(stackNames))
	var wg sync.WaitGroup

	// Process each stack concurrently
	for _, stackName := range stackNames {
		wg.Add(1)
		go func(stackName string) {
			defer wg.Done()

			stack := stacks[stackName]

			// Build inline environment (including decrypted secrets)
			inline := detector.BuildInlineEnv(ctx, stack, cfg.Sops)

			services, err := detector.DetectAllServicesState(ctx, stackName, stack, identifier, cfg.Sops)

			var resources []Resource
			var execData *StackExecutionData

			if err != nil || len(services) == 0 {
				resources = fallbackStackResource()
			} else {
				// Store execution data for reuse during apply
				execData = &StackExecutionData{
					Services:   services,
					InlineEnv:  inline,
					NeedsApply: NeedsApply(services),
				}
				resources = serviceStatesToResources(services)
			}

			resultsChan <- stackResult{stackName: stackName, resources: resources, execData: execData}
		}(stackName)
	}

	// Wait for all stacks to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results and add to plan
	for result := range resultsChan {
		plan.Stacks[result.stackName] = result.resources
		if result.execData != nil {
			execCtx.Stacks[result.stackName] = result.execData
		}
	}

	return nil
}

// collectDesiredServicesForContext returns a map of all service names that should be running for a contextConfig.
func (p *Planner) collectDesiredServicesForContext(ctx context.Context, cfg manifest.Config, stacks map[string]manifest.Stack, client DockerClient) map[string]struct{} {
	desiredServices := map[string]struct{}{}

	if client == nil {
		return desiredServices
	}

	detector := NewServiceStateDetector(client)

	for _, stack := range stacks {
		inline := detector.BuildInlineEnv(ctx, stack, cfg.Sops)
		names, err := detector.GetPlannedServices(ctx, stack, inline)
		if err != nil {
			continue // Skip this stack if we can't list planned services
		}
		for _, name := range names {
			desiredServices[name] = struct{}{}
		}
	}

	return desiredServices
}

// getExistingResourcesForClient fetches volumes and networks for a specific client
func (p *Planner) getExistingResourcesForClient(ctx context.Context, client DockerClient) (volumes, networks map[string]struct{}) {
	volumes = map[string]struct{}{}
	networks = map[string]struct{}{}

	var wg sync.WaitGroup
	var volumesMu, networksMu sync.Mutex

	// Fetch volumes concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		if vols, err := client.ListVolumes(ctx); err == nil {
			volumesMu.Lock()
			for _, v := range vols {
				volumes[v] = struct{}{}
			}
			volumesMu.Unlock()
		}
	}()

	// Fetch networks concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		if nets, err := client.ListNetworks(ctx); err == nil {
			networksMu.Lock()
			for _, n := range nets {
				networks[n] = struct{}{}
			}
			networksMu.Unlock()
		}
	}()

	wg.Wait()
	return volumes, networks
}

// buildFilesetResourcesForContext processes fileset diffs for a context and adds them to the plan.
func (p *Planner) buildFilesetResourcesForContext(ctx context.Context, filesetSpecs map[string]manifest.FilesetSpec, existingVolumes map[string]struct{}, client DockerClient, plan *ResourcePlan, execCtx *ContextExecutionContext) {
	filesetNames := sortedKeys(filesetSpecs)
	if len(filesetNames) == 0 {
		return
	}

	type filesetResult struct {
		name      string
		resources []Resource
		execData  *FilesetExecutionData
	}

	resultsChan := make(chan filesetResult, len(filesetNames))
	var wg sync.WaitGroup

	// Process each fileset concurrently
	for _, name := range filesetNames {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			a := filesetSpecs[name]
			var resources []Resource

			// Build local index
			local, err := filesets.BuildLocalIndex(a.SourceAbs, a.TargetPath, a.Exclude)
			if err != nil {
				resources = append(resources,
					NewResource(ResourceFile, "", ActionUpdate,
						fmt.Sprintf("unable to index local files: %v", err)))
				resultsChan <- filesetResult{name: name, resources: resources, execData: nil}
				return
			}

			// Read remote index only if the target volume exists
			raw := ""
			if _, volumeExists := existingVolumes[a.TargetVolume]; volumeExists {
				raw, _ = client.ReadFileFromVolume(ctx, a.TargetVolume, a.TargetPath, filesets.IndexFileName)
			}
			remote, _ := filesets.ParseIndexJSON(raw)
			diff := filesets.DiffIndexes(local, remote)

			// Store execution data for reuse during apply
			execData := &FilesetExecutionData{
				LocalIndex:  local,
				RemoteIndex: remote,
				Diff:        diff,
			}

			if local.TreeHash == remote.TreeHash {
				resources = append(resources,
					NewResource(ResourceFile, "", ActionNoop, "no file changes"))
			} else {
				for _, f := range diff.ToCreate {
					resources = append(resources,
						NewResource(ResourceFile, f.Path, ActionCreate, ""))
				}
				for _, f := range diff.ToUpdate {
					resources = append(resources,
						NewResource(ResourceFile, f.Path, ActionUpdate, ""))
				}
				for _, pth := range diff.ToDelete {
					resources = append(resources,
						NewResource(ResourceFile, pth, ActionDelete, ""))
				}
				if len(diff.ToCreate) == 0 && len(diff.ToUpdate) == 0 && len(diff.ToDelete) == 0 {
					resources = append(resources,
						NewResource(ResourceFile, "", ActionUpdate, "changes detected (details unavailable)"))
				}
			}

			resultsChan <- filesetResult{name: name, resources: resources, execData: execData}
		}(name)
	}

	// Wait for all filesets to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results and add to plan and execution context
	for result := range resultsChan {
		plan.Filesets[result.name] = result.resources
		if result.execData != nil {
			execCtx.Filesets[result.name] = result.execData
		}
	}
}

// aggregateContextPlan merges a context plan into the aggregated plan.
func (p *Planner) aggregateContextPlan(aggregated *ResourcePlan, contextPlan *ContextPlan) {
	if contextPlan == nil || contextPlan.Resources == nil {
		return
	}

	dp := contextPlan.Resources

	// Volumes
	aggregated.Volumes = append(aggregated.Volumes, dp.Volumes...)

	// Networks
	aggregated.Networks = append(aggregated.Networks, dp.Networks...)

	// Stacks - prefix with context name for unique keys
	for stackName, resources := range dp.Stacks {
		fullKey := manifest.MakeStackKey(contextPlan.ContextName, stackName)
		aggregated.Stacks[fullKey] = resources
	}

	// Filesets - keys already include context prefix from discovery (daemon/stack/volume)
	for filesetName, resources := range dp.Filesets {
		aggregated.Filesets[filesetName] = resources
	}

	// Containers
	aggregated.Containers = append(aggregated.Containers, dp.Containers...)
}
