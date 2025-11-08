package planner

import (
	"context"
	"strings"

	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/manifest"
)

// ProgressEstimator handles estimation of total work items for progress tracking.
type ProgressEstimator struct {
	docker   DockerClient
	progress ProgressReporter
	execCtx  *ExecutionContext
}

// NewProgressEstimator creates a new progress estimator.
func NewProgressEstimator(docker DockerClient, progress ProgressReporter) *ProgressEstimator {
	return &ProgressEstimator{docker: docker, progress: progress}
}

// WithExecutionContext sets the execution context for reusing pre-computed data.
func (pe *ProgressEstimator) WithExecutionContext(execCtx *ExecutionContext) *ProgressEstimator {
	pe.execCtx = execCtx
	return pe
}

// EstimateAndStartProgress calculates the total number of work items and starts the progress tracker.
func (pe *ProgressEstimator) EstimateAndStartProgress(ctx context.Context, cfg manifest.Config, identifier string) error {
	if pe.progress == nil {
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

	// Count stacks requiring compose up
	stackCount, err := pe.countStacksToUpdate(ctx, cfg, identifier)
	if err != nil {
		return err
	}
	total += stackCount

	// Count service restarts needed
	restartCount, err := pe.countServiceRestarts(ctx, cfg)
	if err != nil {
		return err
	}
	total += restartCount

	if total > 0 {
		pe.progress.Start(total)
	}

	return nil
}

// countVolumesToCreate counts how many volumes need to be created.
func (pe *ProgressEstimator) countVolumesToCreate(ctx context.Context, cfg manifest.Config) (int, error) {
	// Use cached data from execution context if available
	existingVolumes := map[string]struct{}{}
	if pe.execCtx != nil && pe.execCtx.ExistingVolumes != nil {
		existingVolumes = pe.execCtx.ExistingVolumes
	} else if pe.docker != nil {
		// Fallback: query Docker
		if vols, err := pe.docker.ListVolumes(ctx); err == nil {
			for _, v := range vols {
				existingVolumes[v] = struct{}{}
			}
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
	// If we have cached fileset data from BuildPlan, use it to avoid redundant work
	if pe.execCtx != nil && pe.execCtx.Filesets != nil {
		count := 0
		for name := range cfg.Filesets {
			if execData := pe.execCtx.Filesets[name]; execData != nil {
				// Count if tree hashes differ (fileset needs updates)
				if execData.LocalIndex.TreeHash != execData.RemoteIndex.TreeHash {
					count++
				}
			}
		}
		return count, nil
	}

	// Fallback: compute fresh (original behavior)
	existingVolumes := map[string]struct{}{}
	if pe.docker != nil {
		if vols, err := pe.docker.ListVolumes(ctx); err == nil {
			for _, v := range vols {
				existingVolumes[v] = struct{}{}
			}
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
		if pe.docker != nil {
			if _, volumeExists := existingVolumes[fileset.TargetVolume]; volumeExists {
				raw, _ = pe.docker.ReadFileFromVolume(ctx, fileset.TargetVolume, fileset.TargetPath, filesets.IndexFileName)
			}
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
	// Use cached data from execution context if available
	existingNetworks := map[string]struct{}{}
	if pe.execCtx != nil && pe.execCtx.ExistingNetworks != nil {
		existingNetworks = pe.execCtx.ExistingNetworks
	} else if pe.docker != nil {
		// Fallback: query Docker
		if nets, err := pe.docker.ListNetworks(ctx); err == nil {
			for _, n := range nets {
				existingNetworks[n] = struct{}{}
			}
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

// countStacksToUpdate counts how many stacks need compose up.
func (pe *ProgressEstimator) countStacksToUpdate(ctx context.Context, cfg manifest.Config, identifier string) (int, error) {
	if pe.docker == nil {
		return 0, nil
	}

	count := 0

	// If we have execution context from BuildPlan, reuse it to avoid redundant state detection
	if pe.execCtx != nil && pe.execCtx.Stacks != nil {
		for stackName := range cfg.Stacks {
			if execData := pe.execCtx.Stacks[stackName]; execData != nil && execData.NeedsApply {
				count++
			}
		}
		return count, nil
	}

	// Fallback: detect state fresh (original behavior)
	detector := NewServiceStateDetector(pe.docker)
	for stackName, stack := range cfg.Stacks {
		services, err := detector.DetectAllServicesState(ctx, stackName, stack, identifier, cfg.Sops)
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

	// Collect unique restart services using resolution semantics
	restartServices := map[string]struct{}{}
	for _, fs := range cfg.Filesets {
		targets, _ := resolveTargetServices(ctx, pe.docker, fs)
		for _, svc := range targets {
			if strings.TrimSpace(svc) != "" {
				restartServices[svc] = struct{}{}
			}
		}
	}

	if len(restartServices) == 0 {
		return 0, nil
	}

	// Count how many of these services actually exist
	if pe.docker == nil {
		return 0, nil
	}
	items, err := pe.docker.ListComposeContainersAll(ctx)
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
