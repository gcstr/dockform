package planner

import (
	"context"

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
	return &ResourceManager{docker: docker, progress: progress}
}

// NewResourceManagerWithClient creates a new resource manager with a specific client.
func NewResourceManagerWithClient(client DockerClient, progress ProgressReporter) *ResourceManager {
	return &ResourceManager{docker: client, progress: progress}
}

// EnsureVolumesExistForDaemon creates any missing volumes for a specific daemon.
// Volumes are derived from filesets targeting this daemon.
func (rm *ResourceManager) EnsureVolumesExistForDaemon(ctx context.Context, cfg manifest.Config, daemonName string, labels map[string]string) (map[string]struct{}, error) {
	log := logger.FromContext(ctx).With("component", "volume", "daemon", daemonName)

	// Get existing volumes
	existingVolumes := map[string]struct{}{}
	if rm.docker == nil {
		return nil, apperr.New("resourcemanager.EnsureVolumesExistForDaemon", apperr.Precondition, "docker client not configured")
	}
	if vols, err := rm.docker.ListVolumes(ctx); err == nil {
		for _, v := range vols {
			existingVolumes[v] = struct{}{}
		}
	} else {
		return nil, apperr.Wrap("resourcemanager.EnsureVolumesExistForDaemon", apperr.External, err, "list volumes")
	}

	// Collect desired volumes from filesets for this daemon
	daemonFilesets := cfg.GetFilesetsForDaemon(daemonName)
	desiredVolumes := map[string]struct{}{}
	for _, fileset := range daemonFilesets {
		desiredVolumes[fileset.TargetVolume] = struct{}{}
	}

	// Create missing volumes
	for name := range desiredVolumes {
		if _, exists := existingVolumes[name]; !exists {
			st := logger.StartStep(log, "volume_ensure", name, "resource_kind", "volume")
			if rm.progress != nil {
				rm.progress.SetAction("creating volume " + name)
			}
			if err := rm.docker.CreateVolume(ctx, name, labels); err != nil {
				return nil, st.Fail(apperr.Wrap("resourcemanager.EnsureVolumesExistForDaemon", apperr.External, err, "create volume %s", name))
			}
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

// EnsureVolumesExist creates any missing volumes derived from filesets.
// Deprecated: Use EnsureVolumesExistForDaemon for multi-daemon support.
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

	// Collect desired volumes from all filesets
	allFilesets := cfg.GetAllFilesets()
	desiredVolumes := map[string]struct{}{}
	for _, fileset := range allFilesets {
		desiredVolumes[fileset.TargetVolume] = struct{}{}
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

// EnsureNetworksExist is no longer used in the new multi-daemon schema.
// Networks are now created by docker compose up.
// Deprecated: Networks are managed by compose in the new schema.
func (rm *ResourceManager) EnsureNetworksExist(ctx context.Context, cfg manifest.Config, labels map[string]string, execCtx *DaemonExecutionContext) error {
	// In the new schema, networks are created by docker compose up.
	// This function is kept for backward compatibility but does nothing.
	return nil
}
