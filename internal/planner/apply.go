package planner

import (
	"bytes"
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/secrets"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/gcstr/dockform/internal/util"
)

// Apply creates missing top-level resources with labels and performs compose up, labeling containers with identifier.
func (p *Planner) Apply(ctx context.Context, cfg manifest.Config) error {
	if p.docker == nil {
		return apperr.New("planner.Apply", apperr.Precondition, "docker client not configured")
	}
	identifier := cfg.Docker.Identifier
	labels := map[string]string{}
	if identifier != "" {
		labels["io.dockform.identifier"] = identifier
	}

	// Queue services that should be restarted due to fileset updates.
	restartPending := map[string]struct{}{}

	// Initialize progress: estimate total work items conservatively and refine later.
	if p.prog != nil {
		total := 0
		// Volumes to create (derived from filesets)
		existingVolumesForCount := map[string]struct{}{}
		if vols, err := p.docker.ListVolumes(ctx); err == nil {
			for _, v := range vols {
				existingVolumesForCount[v] = struct{}{}
			}
		}
		desiredVolumesForCount := map[string]struct{}{}
		for _, fileset := range cfg.Filesets {
			desiredVolumesForCount[fileset.TargetVolume] = struct{}{}
		}
		for name := range desiredVolumesForCount {
			if _, ok := existingVolumesForCount[name]; !ok {
				total++
			}
		}
		// Filesets: only count ones that actually need updates (check now)
		for _, fileset := range cfg.Filesets {
			if fileset.SourceAbs != "" {
				// Quick check if fileset needs updates by comparing tree hashes
				local, err := filesets.BuildLocalIndex(fileset.SourceAbs, fileset.TargetPath, fileset.Exclude)
				if err == nil {
					raw, _ := p.docker.ReadFileFromVolume(ctx, fileset.TargetVolume, fileset.TargetPath, filesets.IndexFileName)
					remote, _ := filesets.ParseIndexJSON(raw)
					// Only count if tree hashes differ (fileset needs updates)
					if local.TreeHash != remote.TreeHash {
						total++
					}
				}
			}
		}
		// Networks to create
		existingNetworksForCount := map[string]struct{}{}
		if nets, err := p.docker.ListNetworks(ctx); err == nil {
			for _, n := range nets {
				existingNetworksForCount[n] = struct{}{}
			}
		}
		for name := range cfg.Networks {
			if _, ok := existingNetworksForCount[name]; !ok {
				total++
			}
		}
		// Applications requiring compose up
		for appName := range cfg.Applications {
			app := cfg.Applications[appName]
			proj := ""
			if app.Project != nil {
				proj = app.Project.Name
			}
			// Build inline env same as later
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
			plannedServices := []string{}
			if doc, err := p.docker.ComposeConfigFull(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline); err == nil {
				for s := range doc.Services {
					plannedServices = append(plannedServices, s)
				}
			}
			if len(plannedServices) == 0 {
				if names, err := p.docker.ComposeConfigServices(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline); err == nil && len(names) > 0 {
					plannedServices = append([]string(nil), names...)
				}
			}
			if len(plannedServices) == 0 {
				continue
			}
			running := map[string]dockercli.ComposePsItem{}
			if items, err := p.docker.ComposePs(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, inline); err == nil {
				for _, it := range items {
					running[it.Service] = it
				}
			}
			applyNeeded := false
			for _, s := range plannedServices {
				it, ok := running[s]
				if !ok {
					applyNeeded = true
					break
				}
				if identifier != "" {
					keys := []string{"io.dockform.identifier", "com.docker.compose.config-hash"}
					labels, _ := p.docker.InspectContainerLabels(ctx, it.Name, keys)
					if v, ok := labels["io.dockform.identifier"]; !ok || v != identifier {
						applyNeeded = true
						break
					}
					if desiredHash, derr := p.docker.ComposeConfigHash(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, s, identifier, inline); derr == nil && desiredHash != "" {
						runningHash := labels["com.docker.compose.config-hash"]
						if runningHash == "" || runningHash != desiredHash {
							applyNeeded = true
							break
						}
					}
				} else {
					if desiredHash, derr := p.docker.ComposeConfigHash(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, s, "", inline); derr == nil && desiredHash != "" {
						labels, _ := p.docker.InspectContainerLabels(ctx, it.Name, []string{"com.docker.compose.config-hash"})
						runningHash := labels["com.docker.compose.config-hash"]
						if runningHash == "" || runningHash != desiredHash {
							applyNeeded = true
							break
						}
					}
				}
			}
			if applyNeeded {
				total++
			}
		}
		// Restarts planned: count unique restart services that exist
		if len(cfg.Filesets) > 0 {
			set := map[string]struct{}{}
			for _, fs := range cfg.Filesets {
				for _, svc := range fs.RestartServices {
					if strings.TrimSpace(svc) != "" {
						set[svc] = struct{}{}
					}
				}
			}
			if len(set) > 0 {
				if items, err := p.docker.ListComposeContainersAll(ctx); err == nil {
					for svc := range set {
						for _, it := range items {
							if it.Service == svc {
								total++
								break
							}
						}
					}
				}
			}
		}
		if total > 0 {
			p.prog.Start(total)
		}
	}

	// Ensure volumes exist (derived from filesets)
	existingVolumes := map[string]struct{}{}
	if vols, err := p.docker.ListVolumes(ctx); err == nil {
		for _, v := range vols {
			existingVolumes[v] = struct{}{}
		}
	} else {
		return apperr.Wrap("planner.Apply", apperr.External, err, "list volumes")
	}
	desiredVolumes := map[string]struct{}{}
	for _, fileset := range cfg.Filesets {
		desiredVolumes[fileset.TargetVolume] = struct{}{}
	}
	for name := range desiredVolumes {
		if _, ok := existingVolumes[name]; !ok {
			if p.prog != nil {
				p.prog.SetAction("creating volume " + name)
			}
			if err := p.docker.CreateVolume(ctx, name, labels); err != nil {
				return apperr.Wrap("planner.Apply", apperr.External, err, "create volume %s", name)
			}
			if p.prog != nil {
				p.prog.Increment()
			}
		}
	}

	// Sync filesets into volumes selectively using index
	if len(cfg.Filesets) > 0 {
		filesetNames := make([]string, 0, len(cfg.Filesets))
		for n := range cfg.Filesets {
			filesetNames = append(filesetNames, n)
		}
		sort.Strings(filesetNames)
		for _, n := range filesetNames {
			a := cfg.Filesets[n]
			if a.SourceAbs == "" {
				return apperr.New("planner.Apply", apperr.InvalidInput, "fileset %s: resolved source path is empty", n)
			}
			// Local and remote indexes
			local, err := filesets.BuildLocalIndex(a.SourceAbs, a.TargetPath, a.Exclude)
			if err != nil {
				return apperr.Wrap("planner.Apply", apperr.Internal, err, "index local filesets for %s", n)
			}
			raw, _ := p.docker.ReadFileFromVolume(ctx, a.TargetVolume, a.TargetPath, filesets.IndexFileName)
			remote, _ := filesets.ParseIndexJSON(raw)
			diff := filesets.DiffIndexes(local, remote)
			// If completely equal, skip
			if local.TreeHash == remote.TreeHash {
				continue
			}
			// Build tar for create+update
			paths := make([]string, 0, len(diff.ToCreate)+len(diff.ToUpdate))
			for _, f := range diff.ToCreate {
				paths = append(paths, f.Path)
			}
			for _, f := range diff.ToUpdate {
				paths = append(paths, f.Path)
			}
			// Deterministic order for tar emission
			sort.Strings(paths)
			if len(paths) > 0 {
				if p.prog != nil {
					p.prog.SetAction("syncing fileset " + n)
				}
				var buf bytes.Buffer
				if err := util.TarFilesToWriter(a.SourceAbs, paths, &buf); err != nil {
					return apperr.Wrap("planner.Apply", apperr.Internal, err, "build tar for fileset %s", n)
				}
				if err := p.docker.ExtractTarToVolume(ctx, a.TargetVolume, a.TargetPath, &buf); err != nil {
					return apperr.Wrap("planner.Apply", apperr.External, err, "extract tar for fileset %s", n)
				}
			}
			// Deletions
			if len(diff.ToDelete) > 0 {
				if p.prog != nil {
					p.prog.SetAction("deleting files from fileset " + n)
				}
				if err := p.docker.RemovePathsFromVolume(ctx, a.TargetVolume, a.TargetPath, diff.ToDelete); err != nil {
					return apperr.Wrap("planner.Apply", apperr.External, err, "delete files for fileset %s", n)
				}
			}
			// Write index last (not part of tree)
			if p.prog != nil {
				p.prog.SetAction("writing index for fileset " + n)
			}
			jsonStr, err := local.ToJSON()
			if err != nil {
				return apperr.Wrap("planner.Apply", apperr.Internal, err, "encode index for %s", n)
			}
			if err := p.docker.WriteFileToVolume(ctx, a.TargetVolume, a.TargetPath, filesets.IndexFileName, jsonStr); err != nil {
				return apperr.Wrap("planner.Apply", apperr.External, err, "write index for fileset %s", n)
			}
			if p.prog != nil {
				p.prog.Increment()
			}

			// Queue restart for configured services. We'll restart after compose up.
			if len(a.RestartServices) > 0 {
				for _, svc := range a.RestartServices {
					if svc == "" {
						continue
					}
					restartPending[svc] = struct{}{}
				}
			}
		}
	}

	// Ensure networks exist
	existingNetworks := map[string]struct{}{}
	if nets, err := p.docker.ListNetworks(ctx); err == nil {
		for _, n := range nets {
			existingNetworks[n] = struct{}{}
		}
	} else {
		return apperr.Wrap("planner.Apply", apperr.External, err, "list networks")
	}
	for name := range cfg.Networks {
		if _, ok := existingNetworks[name]; !ok {
			if p.prog != nil {
				p.prog.SetAction("creating network " + name)
			}
			if err := p.docker.CreateNetwork(ctx, name, labels); err != nil {
				return apperr.Wrap("planner.Apply", apperr.External, err, "create network %s", name)
			}
			if p.prog != nil {
				p.prog.Increment()
			}
		}
	}

	// Compose up each application only when changes are needed
	for appName := range cfg.Applications {
		app := cfg.Applications[appName]
		proj := ""
		if app.Project != nil {
			proj = app.Project.Name
		}
		// Build inline env same as in planning
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

		// Determine planned services for the app
		plannedServices := []string{}
		doc, cfgErr := p.docker.ComposeConfigFull(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline)
		if cfgErr != nil {
			return apperr.Wrap("planner.Apply", apperr.External, cfgErr, "invalid compose file for application %s", appName)
		}
		for s := range doc.Services {
			plannedServices = append(plannedServices, s)
		}
		sort.Strings(plannedServices)
		if len(plannedServices) == 0 {
			names, err := p.docker.ComposeConfigServices(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline)
			if err != nil {
				return apperr.Wrap("planner.Apply", apperr.External, err, "invalid compose file for application %s", appName)
			}
			if len(names) > 0 {
				plannedServices = append([]string(nil), names...)
				sort.Strings(plannedServices)
			}
		}

		// If no services determined, skip compose up entirely for this app
		if len(plannedServices) == 0 {
			continue
		}

		// Build map of running services
		running := map[string]dockercli.ComposePsItem{}
		if items, err := p.docker.ComposePs(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, inline); err == nil {
			for _, it := range items {
				running[it.Service] = it
			}
		}

		// Decide if apply is needed for this app
		applyNeeded := false
		for _, s := range plannedServices {
			it, ok := running[s]
			// Service not running → need apply
			if !ok {
				applyNeeded = true
				break
			}
			// Identifier label mismatch → need apply
			if identifier != "" {
				keys := []string{"io.dockform.identifier", "com.docker.compose.config-hash"}
				labels, _ := p.docker.InspectContainerLabels(ctx, it.Name, keys)
				if v, ok := labels["io.dockform.identifier"]; !ok || v != identifier {
					applyNeeded = true
					break
				}
				// Hash drift → need apply (only when computable)
				desiredHash, derr := p.docker.ComposeConfigHash(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, s, identifier, inline)
				if derr == nil && desiredHash != "" {
					runningHash := labels["com.docker.compose.config-hash"]
					if runningHash == "" || runningHash != desiredHash {
						applyNeeded = true
						break
					}
				}
			} else {
				// No identifier configured; still check hash drift if available
				desiredHash, derr := p.docker.ComposeConfigHash(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, s, "", inline)
				if derr == nil && desiredHash != "" {
					labels, _ := p.docker.InspectContainerLabels(ctx, it.Name, []string{"com.docker.compose.config-hash"})
					runningHash := labels["com.docker.compose.config-hash"]
					if runningHash == "" || runningHash != desiredHash {
						applyNeeded = true
						break
					}
				}
			}
		}

		if !applyNeeded {
			// Nothing to do for this app
			continue
		}

		// Perform compose up
		if p.prog != nil {
			p.prog.SetAction("docker compose up for " + appName)
		}
		if _, err := p.docker.ComposeUp(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, inline); err != nil {
			return apperr.Wrap("planner.Apply", apperr.External, err, "compose up %s", appName)
		}
		if p.prog != nil {
			p.prog.Increment()
		}

		// Best-effort: ensure identifier label is present only for missing ones
		if identifier != "" {
			if items, err := p.docker.ComposePs(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, inline); err == nil {
				for _, it := range items {
					labels, _ := p.docker.InspectContainerLabels(ctx, it.Name, []string{"io.dockform.identifier"})
					if v, ok := labels["io.dockform.identifier"]; !ok || v != identifier {
						_ = p.docker.UpdateContainerLabels(ctx, it.Name, map[string]string{"io.dockform.identifier": identifier})
					}
				}
			}
		}
	}

	// Perform any pending restarts after compose has ensured containers exist.
	if len(restartPending) > 0 {
		items, _ := p.docker.ListComposeContainersAll(ctx)
		// choose printer (Noop if none provided)
		pr := p.pr
		if pr == nil {
			pr = ui.NoopPrinter{}
		}
		for svc := range restartPending {
			found := false
			for _, it := range items {
				if it.Service == svc {
					found = true
					pr.Info("restarting service %s...", svc)
					if p.prog != nil {
						p.prog.SetAction("restarting service " + svc)
					}
					if err := p.docker.RestartContainer(ctx, it.Name); err != nil {
						return apperr.Wrap("planner.Apply", apperr.External, err, "restart service %s", svc)
					}
					if p.prog != nil {
						p.prog.Increment()
					}
				}
			}
			if !found {
				pr.Warn("%s not found.", svc)
			}
		}
	}
	return nil
}
