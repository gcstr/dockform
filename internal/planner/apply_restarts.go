package planner

import (
	"context"
	"sort"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/ui"
)

// RestartManager handles restarting services after fileset updates.
type RestartManager struct {
	docker   DockerClient
	printer  ui.Printer
	progress ProgressReporter
}

// NewRestartManager creates a new restart manager.
func NewRestartManager(docker DockerClient, printer ui.Printer, progress ProgressReporter) *RestartManager {
	return &RestartManager{docker: docker, printer: printer, progress: orNop(progress)}
}

// NewRestartManagerWithClient creates a new restart manager with a specific client.
func NewRestartManagerWithClient(client DockerClient, printer ui.Printer, progress ProgressReporter) *RestartManager {
	return &RestartManager{docker: client, printer: printer, progress: orNop(progress)}
}

// restartTarget identifies a service to restart by the stack that owns it.
// The stack is required: ResourceRef compares all four of its fields, so a
// service ref built without a Parent never matches the line SeedRefs created
// for that service, and the apply view renders a second, top-level
// "(discovered)" entry instead of updating the seeded one.
//
// Stack is empty only when the owning stack genuinely could not be resolved.
// An empty Stack costs one duplicate line; a wrong one resolves some other
// stack's line, which is worse.
type restartTarget struct {
	Stack   string
	Service string
}

// restartRefFor builds the ResourceRef for a restart, matching the shape
// SeedRefs uses for services (seed.go: Parent is the stack name).
func restartRefFor(contextName string, t restartTarget) ResourceRef {
	return ResourceRef{
		Context: contextName,
		Type:    ResourceService,
		Name:    t.Service,
		Parent:  t.Stack,
	}
}

// RestartPendingServices restarts all services queued for restart after fileset updates.
//
// projectToStack is the same project->stack map SyncFilesetsForContext built
// (see stackProjectMap); it lets a target's stack be matched to its actual
// compose project instead of matching containers by service name alone, which
// two stacks sharing a service name would otherwise collide on.
func (rm *RestartManager) RestartPendingServices(ctx context.Context, contextName string, restartPending map[restartTarget]struct{}, projectToStack map[string]string) error {
	if len(restartPending) == 0 {
		return nil
	}
	stackProject := invertProjectToStack(projectToStack)

	log := logger.FromContext(ctx).With("component", "restart")

	// Get all containers
	if rm.docker == nil {
		return apperr.New("restartmanager.RestartPendingServices", apperr.Precondition, "docker client not configured")
	}
	items, _ := rm.docker.ListComposeContainersAll(ctx)

	// Choose printer (Noop if none provided)
	pr := rm.printer
	if pr == nil {
		pr = ui.NoopPrinter{}
	}

	// Restart each pending service in deterministic order: restartPending is a
	// map, and iterating it directly would make restart lines appear in a
	// different order every run. Sorted by stack first so a run's restarts read
	// grouped by the stack that owns them.
	targets := make([]restartTarget, 0, len(restartPending))
	for t := range restartPending {
		targets = append(targets, t)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Stack != targets[j].Stack {
			return targets[i].Stack < targets[j].Stack
		}
		return targets[i].Service < targets[j].Service
	})

	for _, t := range targets {
		svc := t.Service
		found := false
		for _, it := range items {
			if containerMatchesTarget(it, t, stackProject) {
				found = true
				st := logger.StartStep(log, "service_restart", svc, "resource_kind", "service", "container", it.Name)
				pr.Info("restarting service %s...", svc)

				ref := restartRefFor(contextName, t)
				rm.progress.Start(ref, "restarting")

				if err := rm.docker.RestartContainer(ctx, it.Name); err != nil {
					wrapped := apperr.Wrap("restartmanager.RestartPendingServices", apperr.External, err, "restart service %s", svc)
					rm.progress.Fail(ref, wrapped)
					return st.Fail(wrapped)
				}
				rm.progress.Finish(ref, "restarted")

				st.OK(true)
				break
			}
		}

		if !found {
			st := logger.StartStep(log, "service_restart", svc, "resource_kind", "service")
			pr.Warn("%s not found.", svc)
			_ = st.Fail(apperr.New("restartmanager.RestartPendingServices", apperr.NotFound, "service %s not found", svc))
		}
	}

	return nil
}
