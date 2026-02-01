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

	// Create missing volumes
	for name := range desiredVolumes {
		if _, exists := existingVolumes[name]; !exists {
			st := logger.StartStep(log, "volume_ensure", name, "resource_kind", "volume")
			if rm.progress != nil {
				rm.progress.SetAction("creating volume " + name)
			}
			if err := rm.docker.CreateVolume(ctx, name, labels); err != nil {
				return nil, st.Fail(apperr.Wrap("resourcemanager.EnsureVolumesExistForContext", apperr.External, err, "create volume %s", name))
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
// Deprecated: Use EnsureVolumesExistForContext for multi-context support.
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

// EnsureNetworksExist is no longer used in the new multi-context schema.
// Networks are now created by docker compose up.
// Deprecated: Networks are managed by compose in the new schema.
func (rm *ResourceManager) EnsureNetworksExist(ctx context.Context, cfg manifest.Config, labels map[string]string, execCtx *ContextExecutionContext) error {
	// In the new schema, networks are created by docker compose up.
	// This function is kept for backward compatibility but does nothing.
	return nil
}
