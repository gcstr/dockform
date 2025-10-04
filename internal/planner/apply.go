package planner

import (
	"context"
	"sort"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
)

// Apply creates missing top-level resources with labels and performs compose up, labeling containers with identifier.
func (p *Planner) Apply(ctx context.Context, cfg manifest.Config) error {
	log := logger.FromContext(ctx).With("component", "planner")
	st := logger.StartStep(log, "apply_infrastructure", cfg.Docker.Identifier,
		"resource_kind", "infrastructure",
		"volumes", len(cfg.Volumes),
		"networks", len(cfg.Networks),
		"filesets", len(cfg.Filesets),
		"applications", len(cfg.Applications))

	if p.docker == nil {
		return st.Fail(apperr.New("planner.Apply", apperr.Precondition, "docker client not configured"))
	}

	identifier := cfg.Docker.Identifier
	labels := map[string]string{}
	if identifier != "" {
		labels["io.dockform.identifier"] = identifier
	}

	// Initialize progress tracking
	progress := newProgressReporter(p.prog)
	progressEstimator := NewProgressEstimator(p.docker, progress)
	if err := progressEstimator.EstimateAndStartProgress(ctx, cfg, identifier); err != nil {
		return st.Fail(err)
	}

	// Create missing volumes and networks
	resourceManager := NewResourceManager(p.docker, progress)
	existingVolumes, err := resourceManager.EnsureVolumesExist(ctx, cfg, labels)
	if err != nil {
		return st.Fail(err)
	}

	if err := resourceManager.EnsureNetworksExist(ctx, cfg, labels); err != nil {
		return st.Fail(err)
	}

	// Synchronize filesets
	filesetManager := NewFilesetManager(p.docker, progress)
	restartPending, err := filesetManager.SyncFilesets(ctx, cfg, existingVolumes)
	if err != nil {
		return st.Fail(err)
	}

	// Apply application changes
	if err := p.applyApplicationChanges(ctx, cfg, identifier, restartPending, progress); err != nil {
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

// applyApplicationChanges processes applications and performs compose up for those that need updates.
func (p *Planner) applyApplicationChanges(ctx context.Context, cfg manifest.Config, identifier string, restartPending map[string]struct{}, progress ProgressReporter) error {
	detector := NewServiceStateDetector(p.docker)

	// Process applications in sorted order for deterministic behavior
	appNames := make([]string, 0, len(cfg.Applications))
	for name := range cfg.Applications {
		appNames = append(appNames, name)
	}
	sort.Strings(appNames)

	for _, appName := range appNames {
		app := cfg.Applications[appName]

		// Use ServiceStateDetector to analyze service states
		services, err := detector.DetectAllServicesState(ctx, appName, app, identifier, cfg.Sops)
		if err != nil {
			return apperr.Wrap("planner.Apply", apperr.External, err, "failed to detect service states for application %s", appName)
		}

		if len(services) == 0 {
			continue // No services to manage
		}

		// Check if any services need updates
		if !NeedsApply(services) {
			continue // All services are up-to-date
		}

		// Build inline env for compose operations
		inline := detector.BuildInlineEnv(ctx, app, cfg.Sops)

		// Get project name
		proj := ""
		if app.Project != nil {
			proj = app.Project.Name
		}

		// Perform compose up
		if progress != nil {
			progress.SetAction("docker compose up for " + appName)
		}
		if _, err := p.docker.ComposeUp(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, inline); err != nil {
			return apperr.Wrap("planner.Apply", apperr.External, err, "compose up %s", appName)
		}
		if progress != nil {
			progress.Increment()
		}

		// Best-effort: ensure identifier label is present on containers
		// Note: ComposeUp already uses labeled overlay when identifier is set, so this is typically
		// a no-op defensive check. Only updates if label is missing or mismatched.
		if identifier != "" {
			if items, err := p.docker.ComposePs(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, inline); err == nil {
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
