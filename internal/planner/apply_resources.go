package planner

import (
	"context"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
)

// ResourceManager handles creation of top-level resources like volumes and networks.
type ResourceManager struct {
	planner *Planner
}

// NewResourceManager creates a new resource manager.
func NewResourceManager(planner *Planner) *ResourceManager {
	return &ResourceManager{planner: planner}
}

// EnsureVolumesExist creates any missing volumes derived from filesets and explicit volume definitions.
func (rm *ResourceManager) EnsureVolumesExist(ctx context.Context, cfg manifest.Config, labels map[string]string) (map[string]struct{}, error) {
	// Get existing volumes
	existingVolumes := map[string]struct{}{}
	if vols, err := rm.planner.docker.ListVolumes(ctx); err == nil {
		for _, v := range vols {
			existingVolumes[v] = struct{}{}
		}
	} else {
		return nil, apperr.Wrap("resourcemanager.EnsureVolumesExist", apperr.External, err, "list volumes")
	}

	// Collect desired volumes from filesets and explicit definitions
	desiredVolumes := map[string]struct{}{}
	for _, fileset := range cfg.Filesets {
		desiredVolumes[fileset.TargetVolume] = struct{}{}
	}
	for name := range cfg.Volumes {
		desiredVolumes[name] = struct{}{}
	}

	// Create missing volumes
	for name := range desiredVolumes {
		if _, exists := existingVolumes[name]; !exists {
			if rm.planner.prog != nil {
				rm.planner.prog.SetAction("creating volume " + name)
			}
			if err := rm.planner.docker.CreateVolume(ctx, name, labels); err != nil {
				return nil, apperr.Wrap("resourcemanager.EnsureVolumesExist", apperr.External, err, "create volume %s", name)
			}
			if rm.planner.prog != nil {
				rm.planner.prog.Increment()
			}
			// Add to existing volumes map for return value
			existingVolumes[name] = struct{}{}
		}
	}

	return existingVolumes, nil
}

// EnsureNetworksExist creates any missing networks defined in the manifest.
func (rm *ResourceManager) EnsureNetworksExist(ctx context.Context, cfg manifest.Config, labels map[string]string) error {
	// Get existing networks
	existingNetworks := map[string]struct{}{}
	if nets, err := rm.planner.docker.ListNetworks(ctx); err == nil {
		for _, n := range nets {
			existingNetworks[n] = struct{}{}
		}
	} else {
		return apperr.Wrap("resourcemanager.EnsureNetworksExist", apperr.External, err, "list networks")
	}

	// Create missing networks
	for name := range cfg.Networks {
		if _, exists := existingNetworks[name]; !exists {
			if rm.planner.prog != nil {
				rm.planner.prog.SetAction("creating network " + name)
			}
			if err := rm.planner.docker.CreateNetwork(ctx, name, labels); err != nil {
				return apperr.Wrap("resourcemanager.EnsureNetworksExist", apperr.External, err, "create network %s", name)
			}
			if rm.planner.prog != nil {
				rm.planner.prog.Increment()
			}
		}
	}

	return nil
}
