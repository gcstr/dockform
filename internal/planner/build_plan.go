package planner

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/secrets"
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
	if len(cfg.Applications) == 0 {
		lines = append(lines, ui.Line(ui.Noop, "no applications defined"))
	} else {
		appNames := make([]string, 0, len(cfg.Applications))
		for name := range cfg.Applications {
			appNames = append(appNames, name)
		}
		sort.Strings(appNames)
		// Track desired services across all apps (by service name)
		desiredServices := map[string]struct{}{}
		for _, appName := range appNames {
			app := cfg.Applications[appName]
			if p.docker == nil {
				lines = append(lines, ui.Line(ui.Noop, "application %s planned (services diff TBD)", appName))
				continue
			}

			// Build inline env: user inline plus decrypted SOPS
			inline := append([]string(nil), app.EnvInline...)
			ageKeyFile := ""
			if cfg.Sops != nil && cfg.Sops.Age != nil {
				ageKeyFile = cfg.Sops.Age.KeyFile
			}
			for _, pth0 := range app.SopsSecrets {
				pth := pth0
				if pth != "" && !filepath.IsAbs(pth) {
					pth = filepath.Join(app.Root, pth)
				}
				if pairs, err := secrets.DecryptAndParse(ctx, pth, ageKeyFile); err == nil {
					inline = append(inline, pairs...)
				}
			}

			// Validate compose config; if invalid, return error instead of TBD
			doc, derr := p.docker.ComposeConfigFull(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline)
			if derr != nil {
				return nil, apperr.Wrap("planner.BuildPlan", apperr.External, derr, "invalid compose file for application %s", appName)
			}
			plannedServices := sortedKeys(doc.Services)
			// If no services parsed, try listing services; if that fails, treat as invalid
			if len(plannedServices) == 0 {
				names, err := p.docker.ComposeConfigServices(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline)
				if err != nil {
					return nil, apperr.Wrap("planner.BuildPlan", apperr.External, err, "invalid compose file for application %s", appName)
				}
				if len(names) > 0 {
					plannedServices = append([]string(nil), names...)
					sort.Strings(plannedServices)
				}
			}

			// If still nothing, show fallback line
			if len(plannedServices) == 0 {
				lines = append(lines, ui.Line(ui.Noop, "application %s planned (services diff TBD)", appName))
				continue
			}

			running := map[string]dockercli.ComposePsItem{}
			proj := ""
			if app.Project != nil {
				proj = app.Project.Name
			}
			if items, err := p.docker.ComposePs(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, inline); err == nil {
				for _, it := range items {
					running[it.Service] = it
				}
			}

			for _, s := range plannedServices {
				desiredServices[s] = struct{}{}
				// Always compute desired hash first to generate/print overlay for debugging
				projName := ""
				if app.Project != nil {
					projName = app.Project.Name
				}
				desiredHash, derr := p.docker.ComposeConfigHash(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, projName, s, cfg.Docker.Identifier, inline)
				if it, ok := running[s]; ok {
					// Use compose config hash comparison with identifier overlay
					keys := []string{"com.docker.compose.config-hash"}
					if cfg.Docker.Identifier != "" {
						keys = append(keys, "io.dockform.identifier")
					}
					labels, _ := p.docker.InspectContainerLabels(ctx, it.Name, keys)
					if cfg.Docker.Identifier != "" {
						if v, ok := labels["io.dockform.identifier"]; !ok || v != cfg.Docker.Identifier {
							lines = append(lines, ui.Line(ui.Change, "service %s/%s will be reconciled (identifier mismatch)", appName, s))
							continue
						}
					}
					if derr != nil || desiredHash == "" {
						// Fallback if hash unavailable
						lines = append(lines, ui.Line(ui.Noop, "service %s/%s running", appName, s))
						continue
					}
					runningHash := labels["com.docker.compose.config-hash"]
					if runningHash == "" || runningHash != desiredHash {
						lines = append(lines, ui.Line(ui.Change, "service %s/%s config drift (hash)", appName, s))
					} else {
						lines = append(lines, ui.Line(ui.Noop, "service %s/%s up-to-date", appName, s))
					}
				} else {
					lines = append(lines, ui.Line(ui.Add, "service %s/%s will be started", appName, s))
				}
			}
		}
		// Plan removals for labeled containers whose services are not desired
		if p.docker != nil && len(desiredServices) > 0 {
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
