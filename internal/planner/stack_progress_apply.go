package planner

import (
	"context"
	"errors"

	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
)

// newStackTracker builds the live progress tracker for one stack's compose up.
//
// Building it is best-effort. The changed services are the ones SeedRefs
// seeded: every service not already running and up to date. If the stack's
// compose config or project cannot be resolved, the tracker resolves no
// containers — the stack simply shows today's stack-level display — and the
// failure is logged rather than returned, because progress must never fail an
// apply.
func newStackTracker(ctx context.Context, client DockerClient, reporter ProgressReporter, contextName, stackName string, stack manifest.Stack, services []ServiceInfo, inline []string) *stackProgress {
	changed := map[string]bool{}
	for _, svc := range services {
		if svc.State != ServiceRunning {
			changed[svc.Name] = true
		}
	}
	var resolver containerResolver
	doc, docErr := client.ComposeConfigFull(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, inline)
	project, projErr := stackComposeProject(ctx, client, stack, inline)
	if docErr == nil && projErr == nil {
		resolver = newContainerResolver(doc, project)
	} else {
		logger.FromContext(ctx).Warn("stack_progress_unresolved",
			"context", contextName, "stack", stackName, "error", errors.Join(docErr, projErr))
	}
	return newStackProgress(reporter, contextName, stackName, changed, resolver)
}
