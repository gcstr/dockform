package planner

import (
	"context"
	"sort"

	"github.com/gcstr/dockform/internal/apperr"
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
	return &ResourceManager{docker: docker, progress: orNop(progress)}
}

// NewResourceManagerWithClient creates a new resource manager with a specific client.
func NewResourceManagerWithClient(client DockerClient, progress ProgressReporter) *ResourceManager {
	return &ResourceManager{docker: client, progress: orNop(progress)}
}

// EnsureVolumesExistForContext creates any missing volumes for a specific context.
// Volumes are derived from filesets targeting this context.
func (rm *ResourceManager) EnsureVolumesExistForContext(ctx context.Context, cfg manifest.Config, contextName string, labels map[string]string) (map[string]struct{}, error) {
	log := logger.FromContext(ctx).With("component", "volume", "context", contextName)

	// Get existing volumes
	existingVolumes := map[string]struct{}{}
	if rm.docker == nil {
		return nil, apperr.New("resourcemanager.EnsureVolumesExistForContext", apperr.Precondition, "docker client not configured")
	}
	if vols, err := rm.docker.ListVolumes(ctx); err == nil {
		for _, v := range vols {
			existingVolumes[v] = struct{}{}
		}
	} else {
		return nil, apperr.Wrap("resourcemanager.EnsureVolumesExistForContext", apperr.External, err, "list volumes")
	}

	// Collect desired volumes from filesets for this context
	contextFilesets := cfg.GetFilesetsForContext(contextName)
	desiredVolumes := map[string]struct{}{}
	for _, fileset := range contextFilesets {
		desiredVolumes[fileset.TargetVolume] = struct{}{}
	}

	// Add explicit volumes declared in context config
	if contextConfig, ok := cfg.Contexts[contextName]; ok {
		for volName := range contextConfig.Volumes {
			desiredVolumes[volName] = struct{}{}
		}
	}

	// Volumes that exist without dockform's identifier label are invisible to
	// ListVolumes above. `docker volume create` is idempotent by name and never
	// applies labels to an existing volume, so creating them would be a no-op
	// repeated on every apply. Skip the create and report the drift instead;
	// adopting one in place is impossible without recreating it (data loss).
	unlabeledVolumes := map[string]struct{}{}
	if allVols, err := rm.docker.ListAllVolumes(ctx); err == nil {
		for _, v := range allVols {
			if _, managed := existingVolumes[v]; !managed {
				unlabeledVolumes[v] = struct{}{}
			}
		}
	} else {
		return nil, apperr.Wrap("resourcemanager.EnsureVolumesExistForContext", apperr.External, err, "list all volumes")
	}

	// Create missing volumes in deterministic order.
	volumeNames := make([]string, 0, len(desiredVolumes))
	for name := range desiredVolumes {
		volumeNames = append(volumeNames, name)
	}
	sort.Strings(volumeNames)

	for _, name := range volumeNames {
		if _, unlabeled := unlabeledVolumes[name]; unlabeled {
			log.Warn("volume_exists_unlabeled", "volume", name)
			st := logger.StartStep(log, "volume_ensure", name, "resource_kind", "volume")
			st.OK(false)
			// Still treat it as present: it is a usable volume, and filesets
			// targeting it must keep syncing exactly as before.
			existingVolumes[name] = struct{}{}
			continue
		}
		if _, exists := existingVolumes[name]; !exists {
			st := logger.StartStep(log, "volume_ensure", name, "resource_kind", "volume")
			ref := ResourceRef{Context: contextName, Type: ResourceVolume, Name: name}
			rm.progress.Start(ref, "creating")
			if err := rm.docker.CreateVolume(ctx, name, labels); err != nil {
				wrapped := apperr.Wrap("resourcemanager.EnsureVolumesExistForContext", apperr.External, err, "create volume %s", name)
				rm.progress.Fail(ref, wrapped)
				return nil, st.Fail(wrapped)
			}
			rm.progress.Finish(ref, "created")
			st.OK(true)
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

// EnsureNetworksExistForContext creates any missing networks declared in the context config.
func (rm *ResourceManager) EnsureNetworksExistForContext(ctx context.Context, cfg manifest.Config, contextName string, labels map[string]string, existingNetworks map[string]struct{}) error {
	log := logger.FromContext(ctx).With("component", "resourcemanager", "context", contextName)

	if rm.docker == nil {
		return apperr.New("resourcemanager.EnsureNetworksExistForContext", apperr.Precondition, "docker client not configured")
	}

	contextConfig, ok := cfg.Contexts[contextName]
	if !ok {
		return nil
	}

	// Get desired networks for this context, in deterministic order.
	netNames := make([]string, 0, len(contextConfig.Networks))
	for netName := range contextConfig.Networks {
		netNames = append(netNames, netName)
	}
	sort.Strings(netNames)

	for _, netName := range netNames {
		if _, exists := existingNetworks[netName]; exists {
			continue // Already exists
		}

		ref := ResourceRef{Context: contextName, Type: ResourceNetwork, Name: netName}
		rm.progress.Start(ref, "creating")

		st := logger.StartStep(log, "network_create", netName,
			"resource_kind", "network")

		if err := rm.docker.CreateNetwork(ctx, netName, labels); err != nil {
			wrapped := apperr.Wrap("resourcemanager.EnsureNetworksExistForContext", apperr.External, err, "create network %s", netName)
			rm.progress.Fail(ref, wrapped)
			return st.Fail(wrapped)
		}
		rm.progress.Finish(ref, "created")

		st.OK(true)
	}

	return nil
}
