package planner

import (
	"context"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/logger"
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
	log := logger.FromContext(ctx).With("component", "volume")

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
			st := logger.StartStep(log, "volume_ensure", name, "resource_kind", "volume")
			if rm.planner.prog != nil {
				rm.planner.prog.SetAction("creating volume " + name)
			}
			if err := rm.planner.docker.CreateVolume(ctx, name, labels); err != nil {
				return nil, st.Fail(apperr.Wrap("resourcemanager.EnsureVolumesExist", apperr.External, err, "create volume %s", name))
			}
			st.OK(true)
			if rm.planner.prog != nil {
				rm.planner.prog.Increment()
			}
			// Add to existing volumes map for return value
			existingVolumes[name] = struct{}{}
		} else {
			// Volume already exists - log as no-change
			st := logger.StartStep(log, "volume_ensure", name, "resource_kind", "volume")
			st.OK(false)
		}
	}

	return existingVolumes, nil
}

// EnsureNetworksExist creates any missing networks defined in the manifest.
func (rm *ResourceManager) EnsureNetworksExist(ctx context.Context, cfg manifest.Config, labels map[string]string) error {
	log := logger.FromContext(ctx).With("component", "network")
	// Get existing networks
	existingNetworks := map[string]struct{}{}
	if nets, err := rm.planner.docker.ListNetworks(ctx); err == nil {
		for _, n := range nets {
			existingNetworks[n] = struct{}{}
		}
	} else {
		return apperr.Wrap("resourcemanager.EnsureNetworksExist", apperr.External, err, "list networks")
	}

	// Create or reconcile networks
	for name, spec := range cfg.Networks {
		_, exists := existingNetworks[name]
		// Map manifest spec to docker opts
		opts := dockercli.NetworkCreateOpts{
			Driver:       spec.Driver,
			Options:      spec.Options,
			Internal:     spec.Internal,
			Attachable:   spec.Attachable,
			IPv6:         spec.IPv6,
			Subnet:       spec.Subnet,
			Gateway:      spec.Gateway,
			IPRange:      spec.IPRange,
			AuxAddresses: spec.AuxAddresses,
		}

		if !exists {
			st := logger.StartStep(log, "network_ensure", name, "resource_kind", "network")
			if rm.planner.prog != nil {
				rm.planner.prog.SetAction("creating network " + name)
			}
			if err := rm.planner.docker.CreateNetwork(ctx, name, labels, opts); err != nil {
				return st.Fail(apperr.Wrap("resourcemanager.EnsureNetworksExist", apperr.External, err, "create network %s", name))
			}
			st.OK(true)
			if rm.planner.prog != nil {
				rm.planner.prog.Increment()
			}
			continue
		}

		// Exists: detect drift (inspect actual vs desired)
		ni, _ := rm.planner.docker.InspectNetwork(ctx, name)
		drift := false
		if spec.Driver != "" && ni.Driver != spec.Driver {
			drift = true
		}
		if spec.Internal != ni.Internal || spec.Attachable != ni.Attachable || spec.IPv6 != ni.EnableIPv6 {
			drift = true
		}
		// Compare desired options as subset
		if len(spec.Options) > 0 {
			for k, v := range spec.Options {
				if ni.Options == nil || ni.Options[k] != v {
					drift = true
					break
				}
			}
		}
		// Compare IPAM first config
		if spec.Subnet != "" || spec.Gateway != "" || spec.IPRange != "" || len(spec.AuxAddresses) > 0 {
			var cfg0 dockercli.NetworkInspectIPAMConfig
			if len(ni.IPAM.Config) > 0 {
				cfg0 = ni.IPAM.Config[0]
			}
			if (spec.Subnet != "" && cfg0.Subnet != spec.Subnet) ||
				(spec.Gateway != "" && cfg0.Gateway != spec.Gateway) ||
				(spec.IPRange != "" && cfg0.IPRange != spec.IPRange) {
				drift = true
			}
			if !drift && len(spec.AuxAddresses) > 0 {
				for k, v := range spec.AuxAddresses {
					if cfg0.AuxAddresses == nil || cfg0.AuxAddresses[k] != v {
						drift = true
						break
					}
				}
			}
		}

		if drift {
			st := logger.StartStep(log, "network_recreate", name, "resource_kind", "network", "reason", "drift_detected")
			// Ensure only our containers are attached; abort if others present
			for _, container := range ni.Containers {
				labels, _ := rm.planner.docker.InspectContainerLabels(ctx, container.Name, []string{"io.dockform.identifier"})
				if v, ok := labels["io.dockform.identifier"]; !ok || v != cfg.Docker.Identifier {
					return st.Fail(apperr.New("resourcemanager.EnsureNetworksExist", apperr.Precondition, "network %s in use by unmanaged container %s", name, container.Name))
				}
			}
			// Remove our containers so compose can recreate them
			for _, container := range ni.Containers {
				_ = rm.planner.docker.RemoveContainer(ctx, container.Name, true)
			}
			if rm.planner.prog != nil {
				rm.planner.prog.SetAction("recreating network " + name)
			}
			if err := rm.planner.docker.RemoveNetwork(ctx, name); err != nil {
				return st.Fail(apperr.Wrap("resourcemanager.EnsureNetworksExist", apperr.External, err, "remove network %s", name))
			}
			if err := rm.planner.docker.CreateNetwork(ctx, name, labels, opts); err != nil {
				return st.Fail(apperr.Wrap("resourcemanager.EnsureNetworksExist", apperr.External, err, "recreate network %s", name))
			}
			st.OK(true)
			if rm.planner.prog != nil {
				rm.planner.prog.Increment()
			}
		} else {
			// Network exists and matches desired state - log as no-change
			st := logger.StartStep(log, "network_ensure", name, "resource_kind", "network")
			st.OK(false)
		}
	}

	return nil
}
