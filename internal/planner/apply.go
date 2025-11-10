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

// ApplyWithPlan applies the desired state, optionally reusing execution context from a pre-built plan.
// If plan is non-nil and contains ExecutionContext, this avoids redundant Docker API calls, SOPS decryption,
// and compose config parsing by reusing the state detection results from BuildPlan.
func (p *Planner) ApplyWithPlan(ctx context.Context, cfg manifest.Config, plan *Plan) error {
	log := logger.FromContext(ctx).With("component", "planner")
	st := logger.StartStep(log, "apply_infrastructure", cfg.Docker.Identifier,
		"resource_kind", "infrastructure",
		"volumes", len(cfg.Volumes),
		"networks", len(cfg.Networks),
		"filesets", len(cfg.Filesets),
		"stacks", len(cfg.Stacks))

	if p.docker == nil {
		return st.Fail(apperr.New("planner.Apply", apperr.Precondition, "docker client not configured"))
	}

	identifier := cfg.Docker.Identifier
	labels := map[string]string{}
	if identifier != "" {
		labels["io.dockform.identifier"] = identifier
	}

	// Extract execution context from plan if available
	var execCtx *ExecutionContext
	if plan != nil && plan.ExecutionContext != nil {
		execCtx = plan.ExecutionContext
	}

	// Initialize progress tracking
	progress := newProgressReporter(p.spinner, p.spinnerPrefix)
	progressEstimator := NewProgressEstimator(p.docker, progress)
	if execCtx != nil {
		progressEstimator = progressEstimator.WithExecutionContext(execCtx)
	}
	if err := progressEstimator.EstimateAndStartProgress(ctx, cfg, identifier); err != nil {
		return st.Fail(err)
	}

	// Create missing volumes and networks
	resourceManager := NewResourceManager(p.docker, progress)
	existingVolumes, err := resourceManager.EnsureVolumesExist(ctx, cfg, labels)
	if err != nil {
		return st.Fail(err)
	}

	if err := resourceManager.EnsureNetworksExist(ctx, cfg, labels, execCtx); err != nil {
		return st.Fail(err)
	}

	// Synchronize filesets
	// Use fresh existingVolumes from EnsureVolumesExist (includes newly created volumes)
	filesetManager := NewFilesetManager(p.docker, progress)
	restartPending, err := filesetManager.SyncFilesets(ctx, cfg, existingVolumes, execCtx)
	if err != nil {
		return st.Fail(err)
	}

	// Apply stack changes (reusing execution context if available)
	if err := p.applyStackChanges(ctx, cfg, identifier, restartPending, progress, execCtx); err != nil {
		return st.Fail(err)
	}

	// Restart services that need it
	restartManager := NewRestartManager(p.docker, p.pr, progress)
	if err := restartManager.RestartPendingServices(ctx, restartPending); err != nil {
		return st.Fail(err)
	}

	st.OK(true)
	return nil
}

// applyStackChanges processes stacks and performs compose up for those that need updates.
// If execCtx is non-nil, it reuses pre-computed state detection results to avoid redundant work.
func (p *Planner) applyStackChanges(ctx context.Context, cfg manifest.Config, identifier string, restartPending map[string]struct{}, progress ProgressReporter, execCtx *ExecutionContext) error {
	detector := NewServiceStateDetector(p.docker)

	// Process stacks in sorted order for deterministic behavior
	stackNames := make([]string, 0, len(cfg.Stacks))
	for name := range cfg.Stacks {
		stackNames = append(stackNames, name)
	}
	sort.Strings(stackNames)

	for _, stackName := range stackNames {
		stack := cfg.Stacks[stackName]

		var services []ServiceInfo
		var inline []string
		var needsApply bool

		// Check if we have pre-computed execution data from BuildPlan
		if execCtx != nil && execCtx.Stacks[stackName] != nil {
			// Reuse pre-computed data to avoid redundant state detection
			log := logger.FromContext(ctx)
			log.Info("apply_stack_reuse_cache", "stack", stackName, "msg", "reusing execution context from plan")
			execData := execCtx.Stacks[stackName]
			services = execData.Services
			inline = execData.InlineEnv
			needsApply = execData.NeedsApply
		} else {
			// Fallback: detect state fresh (original behavior)
			var err error
			services, err = detector.DetectAllServicesState(ctx, stackName, stack, identifier, cfg.Sops)
			if err != nil {
				return apperr.Wrap("planner.Apply", apperr.External, err, "failed to detect service states for stack %s", stackName)
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
			progress.SetAction("docker compose up for " + stackName)
		}
		if _, err := p.docker.ComposeUp(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, proj, inline); err != nil {
			return apperr.Wrap("planner.Apply", apperr.External, err, "compose up %s", stackName)
		}

		// Best-effort: ensure identifier label is present on containers
		// Note: ComposeUp already uses labeled overlay when identifier is set, so this is typically
		// a no-op defensive check. Only updates if label is missing or mismatched.
		if identifier != "" {
			if items, err := p.docker.ComposePs(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, proj, inline); err == nil {
				for _, it := range items {
					labels, _ := p.docker.InspectContainerLabels(ctx, it.Name, []string{"io.dockform.identifier"})
					if v, ok := labels["io.dockform.identifier"]; !ok || v != identifier {
						_ = p.docker.UpdateContainerLabels(ctx, it.Name, map[string]string{"io.dockform.identifier": identifier})
					}
				}
			}
		}
	}

	return nil
}
