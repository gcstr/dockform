package planner

import (
	"context"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/ui"
)

// RestartManager handles restarting services after fileset updates.
type RestartManager struct {
	planner *Planner
}

// NewRestartManager creates a new restart manager.
func NewRestartManager(planner *Planner) *RestartManager {
	return &RestartManager{planner: planner}
}

// RestartPendingServices restarts all services queued for restart after fileset updates.
func (rm *RestartManager) RestartPendingServices(ctx context.Context, restartPending map[string]struct{}) error {
	if len(restartPending) == 0 {
		return nil
	}

	// Get all containers
	items, _ := rm.planner.docker.ListComposeContainersAll(ctx)
	
	// Choose printer (Noop if none provided)
	pr := rm.planner.pr
	if pr == nil {
		pr = ui.NoopPrinter{}
	}

	// Restart each pending service
	for svc := range restartPending {
		found := false
		for _, it := range items {
			if it.Service == svc {
				found = true
				pr.Info("restarting service %s...", svc)
				
				if rm.planner.prog != nil {
					rm.planner.prog.SetAction("restarting service " + svc)
				}
				
				if err := rm.planner.docker.RestartContainer(ctx, it.Name); err != nil {
					return apperr.Wrap("restartmanager.RestartPendingServices", apperr.External, err, "restart service %s", svc)
				}
				
				if rm.planner.prog != nil {
					rm.planner.prog.Increment()
				}
				break
			}
		}
		
		if !found {
			pr.Warn("%s not found.", svc)
		}
	}

	return nil
}
