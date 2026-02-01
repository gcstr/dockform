package planner

import (
	"context"
	"fmt"
	"sync"

	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/manifest"
)

// buildFilesetResourcesForContext processes fileset diffs for a context and adds them to the plan.
func (p *Planner) buildFilesetResourcesForContext(ctx context.Context, filesetSpecs map[string]manifest.FilesetSpec, existingVolumes map[string]struct{}, client DockerClient, plan *ResourcePlan, execCtx *ContextExecutionContext) {
	filesetNames := sortedKeys(filesetSpecs)
	if len(filesetNames) == 0 {
		return
	}

	type filesetResult struct {
		name      string
		resources []Resource
		execData  *FilesetExecutionData
	}

	resultsChan := make(chan filesetResult, len(filesetNames))
	var wg sync.WaitGroup

	// Process each fileset concurrently
	for _, name := range filesetNames {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			a := filesetSpecs[name]
			var resources []Resource

			// Build local index
			local, err := filesets.BuildLocalIndex(a.SourceAbs, a.TargetPath, a.Exclude)
			if err != nil {
				resources = append(resources,
					NewResource(ResourceFile, "", ActionUpdate,
						fmt.Sprintf("unable to index local files: %v", err)))
				resultsChan <- filesetResult{name: name, resources: resources, execData: nil}
				return
			}

			// Read remote index only if the target volume exists
			raw := ""
			if _, volumeExists := existingVolumes[a.TargetVolume]; volumeExists {
				raw, _ = client.ReadFileFromVolume(ctx, a.TargetVolume, a.TargetPath, filesets.IndexFileName)
			}
			remote, _ := filesets.ParseIndexJSON(raw)
			diff := filesets.DiffIndexes(local, remote)

			// Store execution data for reuse during apply
			execData := &FilesetExecutionData{
				LocalIndex:  local,
				RemoteIndex: remote,
				Diff:        diff,
			}

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

			resultsChan <- filesetResult{name: name, resources: resources, execData: execData}
		}(name)
	}

	// Wait for all filesets to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results and add to plan and execution context
	for result := range resultsChan {
		plan.Filesets[result.name] = result.resources
		if result.execData != nil {
			execCtx.Filesets[result.name] = result.execData
		}
	}
}

// getExistingResourcesForClient fetches volumes and networks for a specific client
func (p *Planner) getExistingResourcesForClient(ctx context.Context, client DockerClient) (volumes, networks map[string]struct{}) {
	volumes = map[string]struct{}{}
	networks = map[string]struct{}{}

	var wg sync.WaitGroup
	var volumesMu, networksMu sync.Mutex

	// Fetch volumes concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		if vols, err := client.ListVolumes(ctx); err == nil {
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
		if nets, err := client.ListNetworks(ctx); err == nil {
			networksMu.Lock()
			for _, n := range nets {
				networks[n] = struct{}{}
			}
			networksMu.Unlock()
		}
	}()

	wg.Wait()
	return volumes, networks
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
