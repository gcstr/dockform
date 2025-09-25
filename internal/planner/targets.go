package planner

import (
	"context"
	"sort"

	"github.com/gcstr/dockform/internal/manifest"
)

// resolveTargetServices determines the list of service names to act on for a fileset,
// based on the restart_services setting (attached sentinel or explicit list).
func resolveTargetServices(ctx context.Context, docker DockerClient, fs manifest.FilesetSpec) ([]string, error) {
	// Explicit list
	if !fs.RestartServices.Attached {
		if len(fs.RestartServices.Services) == 0 {
			return nil, nil
		}
		// Return a copy to avoid callers modifying underlying slice
		out := make([]string, 0, len(fs.RestartServices.Services))
		seen := map[string]struct{}{}
		for _, s := range fs.RestartServices.Services {
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
		return out, nil
	}

	// Attached discovery: find compose services that have containers using the volume
	if docker == nil {
		return nil, nil
	}
	// Containers referencing volume (running or stopped)
	volNames, err := docker.ListContainersUsingVolume(ctx, fs.TargetVolume)
	if err != nil {
		// treat errors as no discovery rather than fatal
		return nil, nil
	}
	if len(volNames) == 0 {
		return nil, nil
	}

	// Map container name -> service via compose labels
	items, err := docker.ListComposeContainersAll(ctx)
	if err != nil {
		return nil, nil
	}
	// Build set of volume containers for fast lookup
	volSet := map[string]struct{}{}
	for _, n := range volNames {
		volSet[n] = struct{}{}
	}
	// Collect services whose container name is in volSet
	serviceSet := map[string]struct{}{}
	for _, it := range items {
		if _, ok := volSet[it.Name]; ok && it.Service != "" {
			serviceSet[it.Service] = struct{}{}
		}
	}
	if len(serviceSet) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(serviceSet))
	for s := range serviceSet {
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}
