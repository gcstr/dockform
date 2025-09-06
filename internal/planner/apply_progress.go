package planner

import (
	"context"
	"strings"

	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/manifest"
)

// ProgressEstimator handles estimation of total work items for progress tracking.
type ProgressEstimator struct {
	planner *Planner
}

// NewProgressEstimator creates a new progress estimator.
func NewProgressEstimator(planner *Planner) *ProgressEstimator {
	return &ProgressEstimator{planner: planner}
}

// EstimateAndStartProgress calculates the total number of work items and starts the progress tracker.
func (pe *ProgressEstimator) EstimateAndStartProgress(ctx context.Context, cfg manifest.Config, identifier string) error {
	if pe.planner.prog == nil {
		return nil // No progress tracking configured
	}

	total := 0

	// Count volumes to create
	volumeCount, err := pe.countVolumesToCreate(ctx, cfg)
	if err != nil {
		return err
	}
	total += volumeCount

	// Count filesets that need updates
	filesetCount, err := pe.countFilesetsToUpdate(ctx, cfg)
	if err != nil {
		return err
	}
	total += filesetCount

	// Count networks to create
	networkCount, err := pe.countNetworksToCreate(ctx, cfg)
	if err != nil {
		return err
	}
	total += networkCount

	// Count applications requiring compose up
	appCount, err := pe.countApplicationsToUpdate(ctx, cfg, identifier)
	if err != nil {
		return err
	}
	total += appCount

	// Count service restarts needed
	restartCount, err := pe.countServiceRestarts(ctx, cfg)
	if err != nil {
		return err
	}
	total += restartCount

	if total > 0 {
		pe.planner.prog.Start(total)
	}
	
	return nil
}

// countVolumesToCreate counts how many volumes need to be created.
func (pe *ProgressEstimator) countVolumesToCreate(ctx context.Context, cfg manifest.Config) (int, error) {
	existingVolumes := map[string]struct{}{}
	if vols, err := pe.planner.docker.ListVolumes(ctx); err == nil {
		for _, v := range vols {
			existingVolumes[v] = struct{}{}
		}
	}

	desiredVolumes := map[string]struct{}{}
	for _, fileset := range cfg.Filesets {
		desiredVolumes[fileset.TargetVolume] = struct{}{}
	}
	for name := range cfg.Volumes {
		desiredVolumes[name] = struct{}{}
	}

	count := 0
	for name := range desiredVolumes {
		if _, exists := existingVolumes[name]; !exists {
			count++
		}
	}

	return count, nil
}

// countFilesetsToUpdate counts how many filesets need synchronization.
func (pe *ProgressEstimator) countFilesetsToUpdate(ctx context.Context, cfg manifest.Config) (int, error) {
	// Get existing volumes to avoid creating them during estimation
	existingVolumes := map[string]struct{}{}
	if vols, err := pe.planner.docker.ListVolumes(ctx); err == nil {
		for _, v := range vols {
			existingVolumes[v] = struct{}{}
		}
	}

	count := 0
	for _, fileset := range cfg.Filesets {
		if fileset.SourceAbs == "" {
			continue
		}

		// Quick check if fileset needs updates by comparing tree hashes
		local, err := filesets.BuildLocalIndex(fileset.SourceAbs, fileset.TargetPath, fileset.Exclude)
		if err != nil {
			continue // Skip on error during estimation
		}

		// Only read remote index when volume exists to avoid implicit creation
		raw := ""
		if _, volumeExists := existingVolumes[fileset.TargetVolume]; volumeExists {
			raw, _ = pe.planner.docker.ReadFileFromVolume(ctx, fileset.TargetVolume, fileset.TargetPath, filesets.IndexFileName)
		}
		remote, _ := filesets.ParseIndexJSON(raw)

		// Only count if tree hashes differ (fileset needs updates)
		if local.TreeHash != remote.TreeHash {
			count++
		}
	}

	return count, nil
}

// countNetworksToCreate counts how many networks need to be created.
func (pe *ProgressEstimator) countNetworksToCreate(ctx context.Context, cfg manifest.Config) (int, error) {
	existingNetworks := map[string]struct{}{}
	if nets, err := pe.planner.docker.ListNetworks(ctx); err == nil {
		for _, n := range nets {
			existingNetworks[n] = struct{}{}
		}
	}

	count := 0
	for name := range cfg.Networks {
		if _, exists := existingNetworks[name]; !exists {
			count++
		}
	}

	return count, nil
}

// countApplicationsToUpdate counts how many applications need compose up.
func (pe *ProgressEstimator) countApplicationsToUpdate(ctx context.Context, cfg manifest.Config, identifier string) (int, error) {
	detector := NewServiceStateDetector(pe.planner.docker)
	count := 0

	for appName, app := range cfg.Applications {
		services, err := detector.DetectAllServicesState(ctx, appName, app, identifier, cfg.Sops)
		if err == nil && NeedsApply(services) {
			count++
		}
	}

	return count, nil
}

// countServiceRestarts counts how many unique services need to be restarted.
func (pe *ProgressEstimator) countServiceRestarts(ctx context.Context, cfg manifest.Config) (int, error) {
	if len(cfg.Filesets) == 0 {
		return 0, nil
	}

	// Collect unique restart services
	restartServices := map[string]struct{}{}
	for _, fs := range cfg.Filesets {
		for _, svc := range fs.RestartServices {
			if strings.TrimSpace(svc) != "" {
				restartServices[svc] = struct{}{}
			}
		}
	}

	if len(restartServices) == 0 {
		return 0, nil
	}

	// Count how many of these services actually exist
	items, err := pe.planner.docker.ListComposeContainersAll(ctx)
	if err != nil {
		return 0, nil // Skip counting on error
	}

	count := 0
	for svc := range restartServices {
		for _, it := range items {
			if it.Service == svc {
				count++
				break
			}
		}
	}

	return count, nil
}
