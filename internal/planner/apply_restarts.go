package planner

import (
	"context"

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
	return &RestartManager{docker: docker, printer: printer, progress: progress}
}

// RestartPendingServices restarts all services queued for restart after fileset updates.
func (rm *RestartManager) RestartPendingServices(ctx context.Context, restartPending map[string]struct{}) error {
	if len(restartPending) == 0 {
		return nil
	}

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

	// Restart each pending service
	for svc := range restartPending {
		found := false
		for _, it := range items {
			if it.Service == svc {
				found = true
				st := logger.StartStep(log, "service_restart", svc, "resource_kind", "service", "container", it.Name)
				pr.Info("restarting service %s...", svc)

				if rm.progress != nil {
					rm.progress.SetAction("restarting service " + svc)
				}

				if err := rm.docker.RestartContainer(ctx, it.Name); err != nil {
					return st.Fail(apperr.Wrap("restartmanager.RestartPendingServices", apperr.External, err, "restart service %s", svc))
				}

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
