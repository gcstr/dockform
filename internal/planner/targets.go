package planner

import (
	"context"
	"sort"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
)

// resolveTargetServices determines the services to act on for a fileset, based
// on the restart_services setting (attached sentinel or explicit list).
//
// Each target carries the stack that owns the service, because the apply view
// keys a service's line on {Context, Type, Name, Parent} and Parent is the
// stack name. The two branches derive that stack differently, and they must:
//
//   - An explicit list names services of the fileset's OWN stack, so
//     fs.Stack is authoritative.
//   - Attached discovery finds services from any container mounting the target
//     volume, which may belong to a DIFFERENT stack than the fileset. fs.Stack
//     would be wrong there, so the container's own compose project is mapped
//     back to a manifest stack key via projectToStack.
//
// projectToStack may be nil; a service whose project cannot be mapped gets an
// empty Stack, which costs one duplicate line rather than resolving some other
// stack's line.
func resolveTargetServices(ctx context.Context, docker DockerClient, fs manifest.FilesetSpec, projectToStack map[string]string) ([]restartTarget, error) {
	// Explicit list
	if !fs.RestartServices.Attached {
		if len(fs.RestartServices.Services) == 0 {
			return nil, nil
		}
		// Return a copy to avoid callers modifying underlying slice
		out := make([]restartTarget, 0, len(fs.RestartServices.Services))
		seen := map[string]struct{}{}
		for _, s := range fs.RestartServices.Services {
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, restartTarget{Stack: fs.Stack, Service: s})
		}
		return out, nil
	}

	// Attached discovery: find compose services that have containers using the volume
	if docker == nil {
		return nil, apperr.New("planner.resolveTargetServices", apperr.Precondition, "docker client not configured")
	}
	// Containers referencing volume (running or stopped)
	volNames, err := docker.ListContainersUsingVolume(ctx, fs.TargetVolume)
	if err != nil {
		return nil, apperr.Wrap("planner.resolveTargetServices", apperr.External, err, "list containers using volume %s", fs.TargetVolume)
	}
	if len(volNames) == 0 {
		return nil, nil
	}

	// Map container name -> service via compose labels
	items, err := docker.ListComposeContainersAll(ctx)
	if err != nil {
		return nil, apperr.Wrap("planner.resolveTargetServices", apperr.External, err, "list compose containers for volume %s", fs.TargetVolume)
	}
	// Build set of volume containers for fast lookup
	volSet := map[string]struct{}{}
	for _, n := range volNames {
		volSet[n] = struct{}{}
	}
	// Collect services whose container name is in volSet. The container's own
	// compose project decides the owning stack: a volume can be mounted by a
	// container from a stack other than the fileset's, and attributing it to
	// fs.Stack would point the apply view at the wrong line.
	seen := map[restartTarget]struct{}{}
	for _, it := range items {
		if _, ok := volSet[it.Name]; !ok || it.Service == "" {
			continue
		}
		// Unmappable project -> empty Stack. One duplicate line beats a line
		// that belongs to somebody else.
		stack := projectToStack[normalizeComposeProject(it.Project)]
		seen[restartTarget{Stack: stack, Service: it.Service}] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, nil
	}
	out := make([]restartTarget, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Stack != out[j].Stack {
			return out[i].Stack < out[j].Stack
		}
		return out[i].Service < out[j].Service
	})
	return out, nil
}

// stackProjectMap maps each stack's resolved compose project name back to its
// manifest stack key, for one context.
//
// It uses stackComposeProject — the same resolver detectStoppedContainer uses —
// so there is one definition of "which compose project is this stack", rather
// than a second fallback that guesses differently. A stack whose project cannot
// be resolved is simply absent from the map, which yields an empty Stack at the
// call site instead of a wrong one.
func stackProjectMap(ctx context.Context, docker DockerClient, stacks map[string]manifest.Stack) map[string]string {
	if len(stacks) == 0 {
		return nil
	}
	out := make(map[string]string, len(stacks))
	for key, stack := range stacks {
		proj, err := stackComposeProject(ctx, docker, stack, nil)
		if err != nil || proj == "" {
			continue
		}
		// Two stacks resolving to the same project is ambiguous; keeping
		// neither is safer than picking one arbitrarily.
		if _, clash := out[proj]; clash {
			out[proj] = ""
			continue
		}
		out[proj] = key
	}
	for proj, key := range out {
		if key == "" {
			delete(out, proj)
		}
	}
	return out
}
