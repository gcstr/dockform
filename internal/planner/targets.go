package planner

import (
	"context"
	"sort"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
)

// normalizeFilesetStack reduces fs.Stack to the bare stack name SeedRefs uses
// as Parent (manifest.GetStacksForContext strips the context via
// ParseStackKey, seed.go:50). fs.Stack itself is populated two different ways
// depending on how the fileset reached the config: loader.go's directory
// discovery already stores the bare name, but validation.go's merge of a
// declared fileset stores the full "context/stack" key. Reducing the keyed
// form here — the only production call site that reads fs.Stack — means both
// paths compare equal to the seeded Parent, not just the discovered one.
func normalizeFilesetStack(raw string) string {
	if _, stack, err := manifest.ParseStackKey(raw); err == nil {
		return stack
	}
	return raw
}

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
			out = append(out, restartTarget{Stack: normalizeFilesetStack(fs.Stack), Service: s})
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
//
// inlineEnv supplies each stack's own compose inline environment, keyed the
// same as stacks. Inline env can set COMPOSE_PROJECT_NAME, so a stack resolved
// with the wrong (or no) inline env can land on a different project than the
// one apply actually runs it under — and if that project happens to match
// another stack's, this map would attribute containers to the wrong stack.
// A missing entry passes nil, which is only correct when the stack truly has
// no inline env.
func stackProjectMap(ctx context.Context, docker DockerClient, contextName string, stacks map[string]manifest.Stack, inlineEnv map[string][]string) map[string]string {
	if len(stacks) == 0 {
		return nil
	}
	out := make(map[string]string, len(stacks))
	for key, stack := range stacks {
		proj, err := stackComposeProject(ctx, docker, stack, inlineEnv[key])
		if err != nil {
			// Matches build_plan_stacks.go's collectDesiredServicesForContext:
			// a failed `compose config` stays diagnosable instead of silently
			// producing an empty Parent.
			logger.FromContext(ctx).Warn("compose_project_unresolved", "context", contextName, "stack", key, "error", err)
			continue
		}
		if proj == "" {
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

// invertProjectToStack builds the reverse of stackProjectMap's project->stack
// map, for call sites that know the stack and need its project (restart
// matching, see containerMatchesTarget). stackProjectMap already drops
// ambiguous projects, so this stays a clean one-to-one mapping.
func invertProjectToStack(projectToStack map[string]string) map[string]string {
	out := make(map[string]string, len(projectToStack))
	for proj, stack := range projectToStack {
		out[stack] = proj
	}
	return out
}

// containerMatchesTarget reports whether it is the container a restart target
// names. The service name must match; when the target's stack maps to a known
// compose project, the container's own project must match it too, so a service
// name shared by two stacks resolves to the container that actually belongs to
// the target's stack instead of whichever one docker lists first.
//
// A target with no known stack (t.Stack == "" or absent from stackProject)
// falls back to matching by service name alone: that is today's behaviour,
// and it is strictly better than refusing to restart anything.
func containerMatchesTarget(it dockercli.PsBrief, t restartTarget, stackProject map[string]string) bool {
	if it.Service != t.Service {
		return false
	}
	if t.Stack == "" {
		return true
	}
	wantProject, ok := stackProject[t.Stack]
	if !ok {
		return true
	}
	return normalizeComposeProject(it.Project) == wantProject
}
