package planner

import (
	"context"
	"sort"
	"sync"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
)

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
		inline, err := detector.BuildInlineEnv(ctx, stack, cfg.Sops)
		if err != nil {
			return apperr.Wrap("planner.buildStackResourcesSequentialForContext", apperr.External, err, "build inline env for stack %s/%s", contextName, stackName)
		}

		services, err := detector.DetectAllServicesState(ctx, stackName, stack, identifier, cfg.Sops)
		if err != nil {
			return apperr.Wrap("planner.buildStackResourcesSequentialForContext", apperr.External, err, "detect service state for stack %s/%s", contextName, stackName)
		}
		if len(services) == 0 {
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
		err       error
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
			inline, err := detector.BuildInlineEnv(ctx, stack, cfg.Sops)
			if err != nil {
				resultsChan <- stackResult{
					stackName: stackName,
					resources: fallbackStackResource(),
					err:       apperr.Wrap("planner.buildStackResourcesParallelForContext", apperr.External, err, "build inline env for stack %s/%s", contextName, stackName),
				}
				return
			}

			services, err := detector.DetectAllServicesState(ctx, stackName, stack, identifier, cfg.Sops)
			if err != nil {
				resultsChan <- stackResult{
					stackName: stackName,
					resources: fallbackStackResource(),
					err:       apperr.Wrap("planner.buildStackResourcesParallelForContext", apperr.External, err, "detect service state for stack %s/%s", contextName, stackName),
				}
				return
			}

			var resources []Resource
			var execData *StackExecutionData

			if len(services) == 0 {
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
	var errs []error
	for result := range resultsChan {
		plan.Stacks[result.stackName] = result.resources
		if result.execData != nil {
			execCtx.Stacks[result.stackName] = result.execData
		}
		if result.err != nil {
			errs = append(errs, result.err)
		}
	}
	if len(errs) > 0 {
		return apperr.Aggregate("planner.buildStackResourcesParallelForContext", apperr.External, "one or more stack analyses failed", errs...)
	}

	return nil
}

// collectDesiredServicesForContext returns a map of all service names that should be running for a contextConfig.
func (p *Planner) collectDesiredServicesForContext(ctx context.Context, cfg manifest.Config, stacks map[string]manifest.Stack, client DockerClient) (map[string]struct{}, error) {
	desiredServices := map[string]struct{}{}

	if client == nil {
		return desiredServices, nil
	}

	detector := NewServiceStateDetector(client)

	for _, stack := range stacks {
		inline, err := detector.BuildInlineEnv(ctx, stack, cfg.Sops)
		if err != nil {
			return nil, err
		}
		names, err := detector.GetPlannedServices(ctx, stack, inline)
		if err != nil {
			return nil, apperr.Wrap("planner.collectDesiredServicesForContext", apperr.External, err, "list planned services for stack %s", stack.Root)
		}
		for _, name := range names {
			desiredServices[name] = struct{}{}
		}
	}

	return desiredServices, nil
}
