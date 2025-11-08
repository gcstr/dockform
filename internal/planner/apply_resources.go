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
	docker   DockerClient
	progress ProgressReporter
}

// NewResourceManager creates a new resource manager.
func NewResourceManager(docker DockerClient, progress ProgressReporter) *ResourceManager {
	return &ResourceManager{docker: docker, progress: progress}
}

// EnsureVolumesExist creates any missing volumes derived from filesets and explicit volume definitions.
func (rm *ResourceManager) EnsureVolumesExist(ctx context.Context, cfg manifest.Config, labels map[string]string) (map[string]struct{}, error) {
	log := logger.FromContext(ctx).With("component", "volume")

	// Get existing volumes
	existingVolumes := map[string]struct{}{}
	if rm.docker == nil {
		return nil, apperr.New("resourcemanager.EnsureVolumesExist", apperr.Precondition, "docker client not configured")
	}
	if vols, err := rm.docker.ListVolumes(ctx); err == nil {
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
			if rm.progress != nil {
				rm.progress.SetAction("creating volume " + name)
			}
			if err := rm.docker.CreateVolume(ctx, name, labels); err != nil {
				return nil, st.Fail(apperr.Wrap("resourcemanager.EnsureVolumesExist", apperr.External, err, "create volume %s", name))
			}
			st.OK(true)
			if rm.progress != nil {
				rm.progress.Increment()
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
// If execCtx is provided with cached network list, it reuses it to avoid redundant ListNetworks call.
func (rm *ResourceManager) EnsureNetworksExist(ctx context.Context, cfg manifest.Config, labels map[string]string, execCtx *ExecutionContext) error {
	log := logger.FromContext(ctx).With("component", "network")
	if rm.docker == nil {
		return apperr.New("resourcemanager.EnsureNetworksExist", apperr.Precondition, "docker client not configured")
	}
	// Get existing networks - use cached data if available
	var existingNetworks map[string]struct{}
	if execCtx != nil && execCtx.ExistingNetworks != nil {
		log.Info("network_ensure_reuse_cache", "msg", "reusing network list from plan")
		existingNetworks = execCtx.ExistingNetworks
	} else {
		// Fallback: query fresh (original behavior)
		existingNetworks = map[string]struct{}{}
		if nets, err := rm.docker.ListNetworks(ctx); err == nil {
			for _, n := range nets {
				existingNetworks[n] = struct{}{}
			}
		} else {
			return apperr.Wrap("resourcemanager.EnsureNetworksExist", apperr.External, err, "list networks")
		}
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
			if rm.progress != nil {
				rm.progress.SetAction("creating network " + name)
			}
			if err := rm.docker.CreateNetwork(ctx, name, labels, opts); err != nil {
				return st.Fail(apperr.Wrap("resourcemanager.EnsureNetworksExist", apperr.External, err, "create network %s", name))
			}
			st.OK(true)
			if rm.progress != nil {
				rm.progress.Increment()
			}
			continue
		}

		// Exists: detect drift (inspect actual vs desired)
		ni, _ := rm.docker.InspectNetwork(ctx, name)
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
				labels, _ := rm.docker.InspectContainerLabels(ctx, container.Name, []string{"io.dockform.identifier"})
				if v, ok := labels["io.dockform.identifier"]; !ok || v != cfg.Docker.Identifier {
					return st.Fail(apperr.New("resourcemanager.EnsureNetworksExist", apperr.Precondition, "network %s in use by unmanaged container %s", name, container.Name))
				}
			}
			// Remove our containers so compose can recreate them
			for _, container := range ni.Containers {
				_ = rm.docker.RemoveContainer(ctx, container.Name, true)
			}
			if rm.progress != nil {
				rm.progress.SetAction("recreating network " + name)
			}
			if err := rm.docker.RemoveNetwork(ctx, name); err != nil {
				return st.Fail(apperr.Wrap("resourcemanager.EnsureNetworksExist", apperr.External, err, "remove network %s", name))
			}
			if err := rm.docker.CreateNetwork(ctx, name, labels, opts); err != nil {
				return st.Fail(apperr.Wrap("resourcemanager.EnsureNetworksExist", apperr.External, err, "recreate network %s", name))
			}
			st.OK(true)
			if rm.progress != nil {
				rm.progress.Increment()
			}
		} else {
			// Network exists and matches desired state - log as no-change
			st := logger.StartStep(log, "network_ensure", name, "resource_kind", "network")
			st.OK(false)
		}
	}

	return nil
}
