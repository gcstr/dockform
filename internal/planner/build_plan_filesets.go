package planner

import (
	"context"
	"sync"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/manifest"
)

// buildFilesetResourcesForContext processes fileset diffs for a context and adds them to the plan.
// Local indexes are built concurrently (CPU-only), but remote index reads run sequentially to
// avoid overwhelming SSH-based Docker contexts with too many concurrent connections.
func (p *Planner) buildFilesetResourcesForContext(ctx context.Context, filesetSpecs map[string]manifest.FilesetSpec, existingVolumes map[string]struct{}, client DockerClient, plan *ResourcePlan, execCtx *ContextExecutionContext) error {
	filesetNames := sortedKeys(filesetSpecs)
	if len(filesetNames) == 0 {
		return nil
	}

	// Phase 1: build all local indexes concurrently (filesystem-only, no SSH).
	type localResult struct {
		name  string
		index filesets.Index
		err   error
	}
	localCh := make(chan localResult, len(filesetNames))
	var wg sync.WaitGroup
	for _, name := range filesetNames {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			a := filesetSpecs[name]
			idx, err := filesets.BuildLocalIndex(a.SourceAbs, a.TargetPath, a.Exclude)
			localCh <- localResult{name: name, index: idx, err: err}
		}(name)
	}
	wg.Wait()
	close(localCh)

	localIndexes := make(map[string]filesets.Index, len(filesetNames))
	var errs []error
	for res := range localCh {
		if res.err != nil {
			plan.Filesets[res.name] = []Resource{
				NewResource(ResourceFile, "", ActionUpdate, "unable to index local files"),
			}
			errs = append(errs, apperr.Wrap("planner.buildFilesetResourcesForContext", apperr.Internal, res.err, "build local fileset index for %s", res.name))
			continue
		}
		localIndexes[res.name] = res.index
	}

	// Phase 2: read remote indexes sequentially to avoid SSH connection floods.
	for _, name := range filesetNames {
		local, ok := localIndexes[name]
		if !ok {
			continue // local index failed above
		}
		a := filesetSpecs[name]

		raw := ""
		if _, volumeExists := existingVolumes[a.TargetVolume]; volumeExists {
			var err error
			raw, err = client.ReadFileFromVolume(ctx, a.TargetVolume, a.TargetPath, filesets.IndexFileName)
			if err != nil {
				plan.Filesets[name] = []Resource{NewResource(ResourceFile, "", ActionUpdate, "unable to read remote index")}
				errs = append(errs, apperr.Wrap("planner.buildFilesetResourcesForContext", apperr.External, err, "read remote index for %s", name))
				continue
			}
		}
		remote, err := filesets.ParseIndexJSON(raw)
		if err != nil {
			plan.Filesets[name] = []Resource{NewResource(ResourceFile, "", ActionUpdate, "unable to parse remote index")}
			errs = append(errs, apperr.Wrap("planner.buildFilesetResourcesForContext", apperr.External, err, "parse remote index for %s", name))
			continue
		}

		diff := filesets.DiffIndexes(local, remote)
		execCtx.Filesets[name] = &FilesetExecutionData{
			LocalIndex:  local,
			RemoteIndex: remote,
			Diff:        diff,
		}

		var resources []Resource
		if local.TreeHash == remote.TreeHash {
			resources = append(resources,
				NewResource(ResourceFile, "", ActionNoop, "no file changes"))
		} else {
			for _, f := range diff.ToCreate {
				resources = append(resources,
					NewResource(ResourceFile, f.Path, ActionCreate, ""))
			}
			for _, f := range diff.ToUpdate {
				resources = append(resources,
					NewResource(ResourceFile, f.Path, ActionUpdate, ""))
			}
			for _, pth := range diff.ToDelete {
				resources = append(resources,
					NewResource(ResourceFile, pth, ActionDelete, ""))
			}
			if len(diff.ToCreate) == 0 && len(diff.ToUpdate) == 0 && len(diff.ToDelete) == 0 {
				resources = append(resources,
					NewResource(ResourceFile, "", ActionUpdate, "changes detected (details unavailable)"))
			}
		}
		plan.Filesets[name] = resources
	}

	return apperr.Aggregate("planner.buildFilesetResourcesForContext", apperr.External, "one or more fileset analyses failed", errs...)
}

// getExistingResourcesForClient fetches volumes and networks for a specific client
func (p *Planner) getExistingResourcesForClient(ctx context.Context, client DockerClient) (volumes, networks map[string]struct{}, err error) {
	volumes = map[string]struct{}{}
	networks = map[string]struct{}{}

	var wg sync.WaitGroup
	var volumesMu, networksMu sync.Mutex
	var errsMu sync.Mutex
	var errs []error

	// Fetch volumes concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		vols, err := client.ListVolumes(ctx)
		if err != nil {
			errsMu.Lock()
			errs = append(errs, apperr.Wrap("planner.getExistingResourcesForClient", apperr.External, err, "list existing volumes"))
			errsMu.Unlock()
			return
		}
		if len(vols) > 0 {
			volumesMu.Lock()
			for _, v := range vols {
				volumes[v] = struct{}{}
			}
			volumesMu.Unlock()
		}
	}()

	// Fetch networks concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		nets, err := client.ListNetworks(ctx)
		if err != nil {
			errsMu.Lock()
			errs = append(errs, apperr.Wrap("planner.getExistingResourcesForClient", apperr.External, err, "list existing networks"))
			errsMu.Unlock()
			return
		}
		if len(nets) > 0 {
			networksMu.Lock()
			for _, n := range nets {
				networks[n] = struct{}{}
			}
			networksMu.Unlock()
		}
	}()

	wg.Wait()
	return volumes, networks, apperr.Aggregate("planner.getExistingResourcesForClient", apperr.External, "failed to discover existing docker resources", errs...)
}

// aggregateContextPlan merges a context plan into the aggregated plan.
func (p *Planner) aggregateContextPlan(aggregated *ResourcePlan, contextPlan *ContextPlan) {
	if contextPlan == nil || contextPlan.Resources == nil {
		return
	}

	dp := contextPlan.Resources

	// Volumes
	aggregated.Volumes = append(aggregated.Volumes, dp.Volumes...)

	// Networks
	aggregated.Networks = append(aggregated.Networks, dp.Networks...)

	// Stacks - prefix with context name for unique keys
	for stackName, resources := range dp.Stacks {
		fullKey := manifest.MakeStackKey(contextPlan.ContextName, stackName)
		aggregated.Stacks[fullKey] = resources
	}

	// Filesets - keys already include context prefix from discovery (daemon/stack/volume)
	for filesetName, resources := range dp.Filesets {
		aggregated.Filesets[filesetName] = resources
	}

	// Containers
	aggregated.Containers = append(aggregated.Containers, dp.Containers...)
}
