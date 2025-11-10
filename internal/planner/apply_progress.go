package planner

import (
	"context"

	"github.com/gcstr/dockform/internal/manifest"
)

// ProgressEstimator handles progress tracking for apply operations.
// It used to estimate total work items for progress bars, but now serves
// as a simple container for the progress reporter (spinner).
type ProgressEstimator struct {
	docker   DockerClient
	progress ProgressReporter
	execCtx  *ExecutionContext
}

// NewProgressEstimator creates a new progress estimator.
func NewProgressEstimator(docker DockerClient, progress ProgressReporter) *ProgressEstimator {
	return &ProgressEstimator{docker: docker, progress: progress}
}

// WithExecutionContext sets the execution context for reusing pre-computed data.
func (pe *ProgressEstimator) WithExecutionContext(execCtx *ExecutionContext) *ProgressEstimator {
	pe.execCtx = execCtx
	return pe
}

// EstimateAndStartProgress is a no-op for spinner-based progress tracking.
// Spinners don't need total work item counts, unlike progress bars.
// This function is kept for API compatibility but does nothing.
func (pe *ProgressEstimator) EstimateAndStartProgress(ctx context.Context, cfg manifest.Config, identifier string) error {
	// Spinner doesn't need total count estimation
	return nil
}
