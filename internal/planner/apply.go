package planner

import (
	"context"
	"sort"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
)

// Apply creates missing top-level resources with labels and performs compose up, labeling containers with identifier.
// This method detects the current state fresh, which may duplicate work if a plan was already built.
// Consider using ApplyWithPlan if you have a pre-built plan to avoid redundant state detection.
func (p *Planner) Apply(ctx context.Context, cfg manifest.Config) error {
	return p.ApplyWithPlan(ctx, cfg, nil)
}

// ApplyWithPlan applies the desired state for all contexts, optionally reusing execution context from a pre-built plan.
// If plan is non-nil and contains ExecutionContext, this avoids redundant Docker API calls, SOPS decryption,
// and compose config parsing by reusing the state detection results from BuildPlan.
func (p *Planner) ApplyWithPlan(ctx context.Context, cfg manifest.Config, plan *Plan) error {
	log := logger.FromContext(ctx).With("component", "planner")

	// Get all stacks and filesets
	allStacks := cfg.GetAllStacks()
	allFilesets := cfg.GetAllFilesets()

	st := logger.StartStep(log, "apply_infrastructure", "multi-context",
		"resource_kind", "infrastructure",
		"contexts", len(cfg.Contexts),
		"stacks", len(allStacks),
		"filesets", len(allFilesets))

	// Process each context (parallel by default, sequential with --sequential)
	err := p.ExecuteAcrossContexts(ctx, &cfg, func(ctx context.Context, contextName string) error {
		contextConfig := cfg.Contexts[contextName]

		// Get Docker client for this context
		client := p.getClientForContext(contextName, &cfg)
		if client == nil {
			return apperr.New("planner.Apply", apperr.Precondition, "docker client not available for context %s", contextName)
		}

		// Get execution context for this context if available
		var contextExecCtx *ContextExecutionContext
		if plan != nil && plan.ExecutionContext != nil {
			contextExecCtx = plan.ExecutionContext.ByContext[contextName]
		}

		return p.applyContext(ctx, cfg, contextName, contextConfig, client, contextExecCtx)
	})
	if err != nil {
		return st.Fail(err)
	}

	st.OK(true)
	return nil
}

// applyContext applies changes for a single context.
func (p *Planner) applyContext(ctx context.Context, cfg manifest.Config, contextName string, contextConfig manifest.ContextConfig, client DockerClient, execCtx *ContextExecutionContext) error {
	log := logger.FromContext(ctx).With("component", "planner", "context", contextName)

	// Get stacks and filesets for this context
	contextStacks := cfg.GetStacksForContext(contextName)
	contextFilesets := cfg.GetFilesetsForContext(contextName)

	st := logger.StartStep(log, "apply_context", contextName,
		"identifier", cfg.Identifier,
		"stacks", len(contextStacks),
		"filesets", len(contextFilesets))

	identifier := cfg.Identifier
	labels := map[string]string{}
	if identifier != "" {
		labels["io.dockform.identifier"] = identifier
	}

	// Initialize progress tracking
	progress := newProgressReporter(p.spinner, p.spinnerPrefix)
	progressEstimator := NewProgressEstimatorWithClient(client, progress)
	if execCtx != nil {
		progressEstimator = progressEstimator.WithExecutionContext(execCtx)
	}
	if err := progressEstimator.EstimateAndStartProgressForContext(ctx, cfg, contextName, identifier); err != nil {
		return st.Fail(err)
	}

	// Create missing volumes
	resourceManager := NewResourceManagerWithClient(client, progress)
	existingVolumes, err := resourceManager.EnsureVolumesExistForContext(ctx, cfg, contextName, labels)
	if err != nil {
		return st.Fail(err)
	}

	// Create missing networks
	existingNetworks := map[string]struct{}{}
	if execCtx != nil {
		existingNetworks = execCtx.ExistingNetworks
	}
	if err := resourceManager.EnsureNetworksExistForContext(ctx, cfg, contextName, labels, existingNetworks); err != nil {
		return st.Fail(err)
	}

	// Synchronize filesets
	filesetManager := NewFilesetManagerWithClient(client, progress)
	restartPending, err := filesetManager.SyncFilesetsForContext(ctx, cfg, contextName, existingVolumes, execCtx)
	if err != nil {
		return st.Fail(err)
	}

	// Apply stack changes (reusing execution context if available)
	if err := p.applyStackChangesForContext(ctx, cfg, contextName, contextStacks, identifier, client, restartPending, progress, execCtx); err != nil {
		return st.Fail(err)
	}

	// Restart services that need it
	restartManager := NewRestartManagerWithClient(client, p.pr, progress)
	if err := restartManager.RestartPendingServices(ctx, restartPending); err != nil {
		return st.Fail(err)
	}

	st.OK(true)
	return nil
}

// applyStackChangesForContext processes stacks for a context and performs compose up for those that need updates.
func (p *Planner) applyStackChangesForContext(ctx context.Context, cfg manifest.Config, contextName string, stacks map[string]manifest.Stack, identifier string, client DockerClient, restartPending map[string]struct{}, progress ProgressReporter, execCtx *ContextExecutionContext) error {
	detector := NewServiceStateDetector(client)

	// Process stacks in sorted order for deterministic behavior
	stackNames := make([]string, 0, len(stacks))
	for name := range stacks {
		stackNames = append(stackNames, name)
	}
	sort.Strings(stackNames)

	for _, stackName := range stackNames {
		stack := stacks[stackName]

		var services []ServiceInfo
		var inline []string
		var needsApply bool

		// Check if we have pre-computed execution data from BuildPlan
		if execCtx != nil && execCtx.Stacks[stackName] != nil {
			// Reuse pre-computed data to avoid redundant state detection
			log := logger.FromContext(ctx)
			log.Info("apply_stack_reuse_cache", "context", contextName, "stack", stackName, "msg", "reusing execution context from plan")
			execData := execCtx.Stacks[stackName]
			services = execData.Services
			inline = execData.InlineEnv
			needsApply = execData.NeedsApply
		} else {
			// Fallback: detect state fresh (original behavior)
			var err error
			services, err = detector.DetectAllServicesState(ctx, stackName, stack, identifier, cfg.Sops)
			if err != nil {
				return apperr.Wrap("planner.Apply", apperr.External, err, "failed to detect service states for stack %s/%s", contextName, stackName)
			}
			inline = detector.BuildInlineEnv(ctx, stack, cfg.Sops)
			needsApply = NeedsApply(services)
		}

		if len(services) == 0 {
			continue // No services to manage
		}

		// Check if any services need updates
		if !needsApply {
			continue // All services are up-to-date
		}

		// Get project name
		proj := ""
		if stack.Project != nil {
			proj = stack.Project.Name
		}

		// Perform compose up
		if progress != nil {
			progress.SetAction("docker compose up for " + contextName + "/" + stackName)
		}
		if _, err := client.ComposeUp(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, proj, inline); err != nil {
			return apperr.Wrap("planner.Apply", apperr.External, err, "compose up %s/%s", contextName, stackName)
		}

		// Best-effort: ensure identifier label is present on containers
		if identifier != "" {
			if items, err := client.ComposePs(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, proj, inline); err == nil {
				for _, it := range items {
					labels, _ := client.InspectContainerLabels(ctx, it.Name, []string{"io.dockform.identifier"})
					if v, ok := labels["io.dockform.identifier"]; !ok || v != identifier {
						_ = client.UpdateContainerLabels(ctx, it.Name, map[string]string{"io.dockform.identifier": identifier})
					}
				}
			}
		}
	}

	return nil
}

