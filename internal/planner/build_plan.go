package planner

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/manifest"
)

// BuildPlan produces a structured plan with resources organized by type.
func (p *Planner) BuildPlan(ctx context.Context, cfg manifest.Config) (*Plan, error) {
	resourcePlan := &ResourcePlan{
		Volumes:      []Resource{},
		Networks:     []Resource{},
		Applications: make(map[string][]Resource),
		Filesets:     make(map[string][]Resource),
		Containers:   []Resource{},
	}

	// Accumulate existing sets when docker client is available
	var existingVolumes, existingNetworks map[string]struct{}
	if p.docker != nil {
		existingVolumes, existingNetworks = p.getExistingResourcesConcurrently(ctx)
	}

	// Plan volumes - combine volumes from filesets and explicit volumes
	desiredVolumes := map[string]struct{}{}
	for _, fileset := range cfg.Filesets {
		desiredVolumes[fileset.TargetVolume] = struct{}{}
	}
	for name := range cfg.Volumes {
		desiredVolumes[name] = struct{}{}
	}

	volNames := sortedKeys(desiredVolumes)
	for _, name := range volNames {
		exists := false
		if existingVolumes != nil {
			_, exists = existingVolumes[name]
		}
		if exists {
			resourcePlan.Volumes = append(resourcePlan.Volumes,
				NewResource(ResourceVolume, name, ActionNoop, "exists"))
		} else {
			resourcePlan.Volumes = append(resourcePlan.Volumes,
				NewResource(ResourceVolume, name, ActionCreate, ""))
		}
	}
	// Plan removals for labeled volumes no longer needed
	for name := range existingVolumes {
		if _, want := desiredVolumes[name]; !want {
			resourcePlan.Volumes = append(resourcePlan.Volumes,
				NewResource(ResourceVolume, name, ActionDelete, ""))
		}
	}

	// Plan networks
	netNames := sortedKeys(cfg.Networks)
	for _, name := range netNames {
		exists := false
		if existingNetworks != nil {
			_, exists = existingNetworks[name]
		}
		if exists {
			resourcePlan.Networks = append(resourcePlan.Networks,
				NewResource(ResourceNetwork, name, ActionNoop, "exists"))
		} else {
			resourcePlan.Networks = append(resourcePlan.Networks,
				NewResource(ResourceNetwork, name, ActionCreate, ""))
		}
	}
	// Plan removals for labeled networks no longer in config
	for name := range existingNetworks {
		if _, want := cfg.Networks[name]; !want {
			resourcePlan.Networks = append(resourcePlan.Networks,
				NewResource(ResourceNetwork, name, ActionDelete, ""))
		}
	}

	// Applications: compose planned vs running diff
	if err := p.buildApplicationResources(ctx, cfg, resourcePlan); err != nil {
		return nil, err
	}

	// Track services that should be removed (group under Applications by project)
	if p.docker != nil {
		desiredServices := p.collectDesiredServices(ctx, cfg)
		// Check for orphaned containers even if no desired services exist
		// This handles the case where the entire applications: map is removed
		if all, err := p.docker.ListComposeContainersAll(ctx); err == nil {
			toDelete := map[string]map[string]struct{}{}
			for _, it := range all {
				if _, want := desiredServices[it.Service]; !want {
					if toDelete[it.Project] == nil {
						toDelete[it.Project] = map[string]struct{}{}
					}
					toDelete[it.Project][it.Service] = struct{}{}
				}
			}
			// Add deletions under Applications section
			for appName, services := range toDelete {
				for svc := range services {
					resourcePlan.Applications[appName] = append(resourcePlan.Applications[appName],
						NewResource(ResourceService, svc, ActionDelete, ""))
				}
			}
		}
	}

	// Filesets: show per-file changes using remote index when available
	if p.docker != nil && len(cfg.Filesets) > 0 {
		p.buildFilesetResources(ctx, cfg.Filesets, existingVolumes, resourcePlan)
	}

	// Check if we have any resources
	hasResources := len(resourcePlan.Volumes) > 0 || len(resourcePlan.Networks) > 0 ||
		len(resourcePlan.Applications) > 0 || len(resourcePlan.Filesets) > 0 ||
		len(resourcePlan.Containers) > 0

	if !hasResources {
		// Add a special "nothing to do" resource
		resourcePlan.Volumes = append(resourcePlan.Volumes,
			NewResource(ResourceVolume, "nothing to do", ActionNoop, "nothing to do"))
	}

	return &Plan{Resources: resourcePlan}, nil
}

// buildApplicationResources analyzes applications and adds service resources to the plan.
func (p *Planner) buildApplicationResources(ctx context.Context, cfg manifest.Config, plan *ResourcePlan) error {
	if len(cfg.Applications) == 0 {
		// No applications to process
		return nil
	}

	if p.docker == nil {
		// Without Docker client, we can only show planned applications
		for appName := range cfg.Applications {
			plan.Applications[appName] = []Resource{
				NewResource(ResourceService, "services", ActionNoop, "planned (services diff TBD)"),
			}
		}
		return nil
	}

	// Choose parallel or sequential processing based on configuration
	if p.parallel {
		return p.buildApplicationResourcesParallel(ctx, cfg, plan)
	}
	return p.buildApplicationResourcesSequential(ctx, cfg, plan)
}

// buildApplicationResourcesSequential processes applications one by one
func (p *Planner) buildApplicationResourcesSequential(ctx context.Context, cfg manifest.Config, plan *ResourcePlan) error {
	detector := NewServiceStateDetector(p.docker)

	// Process applications in sorted order for deterministic output
	appNames := make([]string, 0, len(cfg.Applications))
	for name := range cfg.Applications {
		appNames = append(appNames, name)
	}
	sort.Strings(appNames)

	for _, appName := range appNames {
		app := cfg.Applications[appName]
		services, err := detector.DetectAllServicesState(ctx, appName, app, cfg.Docker.Identifier, cfg.Sops)
		if err != nil {
			// Fallback to "TBD" for any errors during planning
			plan.Applications[appName] = []Resource{
				NewResource(ResourceService, "services", ActionNoop, "planned (services diff TBD)"),
			}
			continue
		}

		if len(services) == 0 {
			plan.Applications[appName] = []Resource{
				NewResource(ResourceService, "services", ActionNoop, "planned (services diff TBD)"),
			}
			continue
		}

		// Convert service states to resources
		var appResources []Resource
		for _, service := range services {
			switch service.State {
			case ServiceMissing:
				appResources = append(appResources,
					NewResource(ResourceService, service.Name, ActionCreate, ""))
			case ServiceIdentifierMismatch:
				appResources = append(appResources,
					NewResource(ResourceService, service.Name, ActionReconcile, "identifier mismatch"))
			case ServiceDrifted:
				appResources = append(appResources,
					NewResource(ResourceService, service.Name, ActionUpdate, "config drift"))
			case ServiceRunning:
				if service.DesiredHash != "" {
					appResources = append(appResources,
						NewResource(ResourceService, service.Name, ActionNoop, "up-to-date"))
				} else {
					// Fallback when hash is unavailable
					appResources = append(appResources,
						NewResource(ResourceService, service.Name, ActionNoop, "running"))
				}
			}
		}
		plan.Applications[appName] = appResources
	}

	return nil
}

// buildApplicationResourcesParallel processes applications concurrently for faster planning
func (p *Planner) buildApplicationResourcesParallel(ctx context.Context, cfg manifest.Config, plan *ResourcePlan) error {
	detector := NewServiceStateDetector(p.docker).WithParallel(true)

	// Sort app names for deterministic processing
	appNames := make([]string, 0, len(cfg.Applications))
	for name := range cfg.Applications {
		appNames = append(appNames, name)
	}
	sort.Strings(appNames)

	type appResult struct {
		appName   string
		resources []Resource
	}

	resultsChan := make(chan appResult, len(appNames))
	var wg sync.WaitGroup

	// Process each application concurrently
	for _, appName := range appNames {
		wg.Add(1)
		go func(appName string) {
			defer wg.Done()

			app := cfg.Applications[appName]
			services, err := detector.DetectAllServicesState(ctx, appName, app, cfg.Docker.Identifier, cfg.Sops)

			var resources []Resource
			if err != nil {
				// Fallback to "TBD" for any errors during planning
				resources = append(resources,
					NewResource(ResourceService, "services", ActionNoop, "planned (services diff TBD)"))
			} else if len(services) == 0 {
				resources = append(resources,
					NewResource(ResourceService, "services", ActionNoop, "planned (services diff TBD)"))
			} else {
				// Convert service states to resources
				for _, service := range services {
					switch service.State {
					case ServiceMissing:
						resources = append(resources,
							NewResource(ResourceService, service.Name, ActionCreate, ""))
					case ServiceIdentifierMismatch:
						resources = append(resources,
							NewResource(ResourceService, service.Name, ActionReconcile, "identifier mismatch"))
					case ServiceDrifted:
						resources = append(resources,
							NewResource(ResourceService, service.Name, ActionUpdate, "config drift"))
					case ServiceRunning:
						if service.DesiredHash != "" {
							resources = append(resources,
								NewResource(ResourceService, service.Name, ActionNoop, "up-to-date"))
						} else {
							// Fallback when hash is unavailable
							resources = append(resources,
								NewResource(ResourceService, service.Name, ActionNoop, "running"))
						}
					}
				}
			}

			resultsChan <- appResult{appName: appName, resources: resources}
		}(appName)
	}

	// Wait for all applications to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results and add to plan
	for result := range resultsChan {
		plan.Applications[result.appName] = result.resources
	}

	return nil
}

// collectDesiredServices returns a map of all service names that should be running.
func (p *Planner) collectDesiredServices(ctx context.Context, cfg manifest.Config) map[string]struct{} {
	desiredServices := map[string]struct{}{}

	if p.docker == nil {
		return desiredServices
	}

	detector := NewServiceStateDetector(p.docker)

	for appName, app := range cfg.Applications {
		services, err := detector.DetectAllServicesState(ctx, appName, app, cfg.Docker.Identifier, cfg.Sops)
		if err != nil {
			continue // Skip this app if we can't detect its services
		}

		for _, service := range services {
			desiredServices[service.Name] = struct{}{}
		}
	}

	return desiredServices
}

// getExistingResourcesConcurrently fetches volumes and networks in parallel
func (p *Planner) getExistingResourcesConcurrently(ctx context.Context) (volumes, networks map[string]struct{}) {
	volumes = map[string]struct{}{}
	networks = map[string]struct{}{}

	var wg sync.WaitGroup
	var volumesMu, networksMu sync.Mutex

	// Fetch volumes concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		if vols, err := p.docker.ListVolumes(ctx); err == nil {
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
		if nets, err := p.docker.ListNetworks(ctx); err == nil {
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

// buildFilesetResources processes fileset diffs in parallel and adds them to the plan
func (p *Planner) buildFilesetResources(ctx context.Context, filesetSpecs map[string]manifest.FilesetSpec, existingVolumes map[string]struct{}, plan *ResourcePlan) {
	filesetNames := sortedKeys(filesetSpecs)
	if len(filesetNames) == 0 {
		return
	}

	type filesetResult struct {
		name      string
		resources []Resource
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
				resultsChan <- filesetResult{name: name, resources: resources}
				return
			}

			// Read remote index only if the target volume exists
			raw := ""
			if _, volumeExists := existingVolumes[a.TargetVolume]; volumeExists {
				raw, _ = p.docker.ReadFileFromVolume(ctx, a.TargetVolume, a.TargetPath, filesets.IndexFileName)
			}
			remote, _ := filesets.ParseIndexJSON(raw)
			diff := filesets.DiffIndexes(local, remote)

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

			resultsChan <- filesetResult{name: name, resources: resources}
		}(name)
	}

	// Wait for all filesets to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results and add to plan
	for result := range resultsChan {
		plan.Filesets[result.name] = result.resources
	}
}
