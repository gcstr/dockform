package planner

import (
	"context"
	"sort"

	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
)

// BuildPlan currently produces a minimal plan for top-level volumes and networks.
// Future: inspect docker for current state and diff services/apps.
func (p *Planner) BuildPlan(ctx context.Context, cfg manifest.Config) (*Plan, error) {
	var lines []ui.DiffLine

	// Accumulate existing sets when docker client is available
	var existingVolumes, existingNetworks map[string]struct{}
	if p.docker != nil {
		existingVolumes = map[string]struct{}{}
		if vols, err := p.docker.ListVolumes(ctx); err == nil {
			for _, v := range vols {
				existingVolumes[v] = struct{}{}
			}
		}
		existingNetworks = map[string]struct{}{}
		if nets, err := p.docker.ListNetworks(ctx); err == nil {
			for _, n := range nets {
				existingNetworks[n] = struct{}{}
			}
		}
	}

	// Deterministic ordering for stable output - combine volumes from filesets and explicit volumes
	desiredVolumes := map[string]struct{}{}
	for _, fileset := range cfg.Filesets {
		desiredVolumes[fileset.TargetVolume] = struct{}{}
	}
	// Add explicit volumes from manifest
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
			lines = append(lines, ui.Line(ui.Noop, "volume %s exists", name))
		} else {
			lines = append(lines, ui.Line(ui.Add, "volume %s will be created", name))
		}
	}
	// Plan removals for labeled volumes no longer needed by any fileset
	for name := range existingVolumes {
		if _, want := desiredVolumes[name]; !want {
			lines = append(lines, ui.Line(ui.Remove, "volume %s will be removed", name))
		}
	}

	netNames := sortedKeys(cfg.Networks)
	for _, name := range netNames {
		exists := false
		if existingNetworks != nil {
			_, exists = existingNetworks[name]
		}
		if exists {
			lines = append(lines, ui.Line(ui.Noop, "network %s exists", name))
		} else {
			lines = append(lines, ui.Line(ui.Add, "network %s will be created", name))
		}
	}
	// Plan removals for labeled networks no longer in config
	for name := range existingNetworks {
		if _, want := cfg.Networks[name]; !want {
			lines = append(lines, ui.Line(ui.Remove, "network %s will be removed", name))
		}
	}

	// Applications: compose planned vs running diff
	appLines, err := p.buildApplicationPlan(ctx, cfg)
	if err != nil {
		return nil, err
	}
	lines = append(lines, appLines...)

	// Track desired services for container removal planning
	if p.docker != nil {
		desiredServices := p.collectDesiredServices(ctx, cfg)
		if len(desiredServices) > 0 {
			if all, err := p.docker.ListComposeContainersAll(ctx); err == nil {
				for _, it := range all {
					if _, want := desiredServices[it.Service]; !want {
						lines = append(lines, ui.Line(ui.Remove, "container %s will be removed", it.Name))
					}
				}
			}
		}
	}

	// Filesets: show per-file changes using remote index when available
	if p.docker != nil && len(cfg.Filesets) > 0 {
		filesetNames := sortedKeys(cfg.Filesets)
		for _, name := range filesetNames {
			a := cfg.Filesets[name]
			// Build local index
			local, err := filesets.BuildLocalIndex(a.SourceAbs, a.TargetPath, a.Exclude)
			if err != nil {
				lines = append(lines, ui.Line(ui.Change, "fileset %s: unable to index local files: %v", name, err))
				continue
			}
			// Read remote index only if the target volume exists with proper labels. Avoid docker run -v implicit creation during plan.
			raw := ""
			if _, volumeExists := existingVolumes[a.TargetVolume]; volumeExists {
				raw, _ = p.docker.ReadFileFromVolume(ctx, a.TargetVolume, a.TargetPath, filesets.IndexFileName)
			}
			remote, _ := filesets.ParseIndexJSON(raw)
			diff := filesets.DiffIndexes(local, remote)
			if local.TreeHash == remote.TreeHash {
				lines = append(lines, ui.Line(ui.Noop, "fileset %s: no file changes", name))
				continue
			}
			for _, f := range diff.ToCreate {
				lines = append(lines, ui.Line(ui.Add, "fileset %s: create %s", name, f.Path))
			}
			for _, f := range diff.ToUpdate {
				lines = append(lines, ui.Line(ui.Change, "fileset %s: update %s", name, f.Path))
			}
			for _, pth := range diff.ToDelete {
				lines = append(lines, ui.Line(ui.Remove, "fileset %s: delete %s", name, pth))
			}
			if len(diff.ToCreate) == 0 && len(diff.ToUpdate) == 0 && len(diff.ToDelete) == 0 {
				lines = append(lines, ui.Line(ui.Change, "fileset %s: changes detected (details unavailable)", name))
			}
		}
	}

	if len(lines) == 0 {
		lines = append(lines, ui.Line(ui.Noop, "nothing to do"))
	}
	return &Plan{Lines: lines}, nil
}

// buildApplicationPlan analyzes applications and returns diff lines for services.
func (p *Planner) buildApplicationPlan(ctx context.Context, cfg manifest.Config) ([]ui.DiffLine, error) {
	if len(cfg.Applications) == 0 {
		return []ui.DiffLine{ui.Line(ui.Noop, "no applications defined")}, nil
	}

	if p.docker == nil {
		// Without Docker client, we can only show planned applications
		var lines []ui.DiffLine
		for appName := range cfg.Applications {
			lines = append(lines, ui.Line(ui.Noop, "application %s planned (services diff TBD)", appName))
		}
		return lines, nil
	}

	detector := NewServiceStateDetector(p.docker)
	var lines []ui.DiffLine

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
			lines = append(lines, ui.Line(ui.Noop, "application %s planned (services diff TBD)", appName))
			continue
		}

		if len(services) == 0 {
			lines = append(lines, ui.Line(ui.Noop, "application %s planned (services diff TBD)", appName))
			continue
		}

		// Convert service states to UI lines
		for _, service := range services {
			switch service.State {
			case ServiceMissing:
				lines = append(lines, ui.Line(ui.Add, "service %s/%s will be started", service.AppName, service.Name))
			case ServiceIdentifierMismatch:
				lines = append(lines, ui.Line(ui.Change, "service %s/%s will be reconciled (identifier mismatch)", service.AppName, service.Name))
			case ServiceDrifted:
				lines = append(lines, ui.Line(ui.Change, "service %s/%s config drift (hash)", service.AppName, service.Name))
			case ServiceRunning:
				if service.DesiredHash != "" {
					lines = append(lines, ui.Line(ui.Noop, "service %s/%s up-to-date", service.AppName, service.Name))
				} else {
					// Fallback when hash is unavailable
					lines = append(lines, ui.Line(ui.Noop, "service %s/%s running", service.AppName, service.Name))
				}
			}
		}
	}

	return lines, nil
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
