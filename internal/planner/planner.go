package planner

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/config"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/secrets"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/gcstr/dockform/internal/util"
)

// Plan represents a set of diff lines to show to the user.
type Plan struct {
	Lines []ui.DiffLine
}

// Planner creates a plan comparing desired and current docker state.
type Planner struct {
	docker *dockercli.Client
}

func New() *Planner { return &Planner{} }

func NewWithDocker(client *dockercli.Client) *Planner { return &Planner{docker: client} }

// BuildPlan currently produces a minimal plan for top-level volumes and networks.
// Future: inspect docker for current state and diff services/apps.
func (p *Planner) BuildPlan(ctx context.Context, cfg config.Config) (*Plan, error) {
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

	// Deterministic ordering for stable output
	volNames := sortedKeys(cfg.Volumes)
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
	// Plan removals for labeled volumes no longer in config
	for name := range existingVolumes {
		if _, want := cfg.Volumes[name]; !want {
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

			// Desired config (images, ports) from compose config
			doc, derr := p.docker.ComposeConfigFull(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline)
			plannedServices := sortedKeys(doc.Services)
			// Fallback to services list if no services parsed or error occurred
			if derr != nil || len(plannedServices) == 0 {
				if names, err := p.docker.ComposeConfigServices(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline); err == nil && len(names) > 0 {
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
			// Read remote index only if the target volume exists. Avoid docker run -v implicit creation during plan.
			raw := ""
			if ok, err := p.docker.VolumeExists(ctx, a.TargetVolume); err == nil && ok {
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

func (pln *Plan) String() string {
	out := ""
	for i, l := range pln.Lines {
		if i > 0 {
			out += "\n"
		}
		out += l.String()
	}
	return out
}

// sortedKeys returns sorted keys of a map[string]T
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Port comparison helpers removed in favor of hash-based comparison.

// Apply creates missing top-level resources with labels and performs compose up, labeling containers with identifier.
func (p *Planner) Apply(ctx context.Context, cfg config.Config) error {
	if p.docker == nil {
		return apperr.New("planner.Apply", apperr.Precondition, "docker client not configured")
	}
	identifier := cfg.Docker.Identifier
	labels := map[string]string{}
	if identifier != "" {
		labels["io.dockform.identifier"] = identifier
	}

	// Ensure volumes exist
	existingVolumes := map[string]struct{}{}
	if vols, err := p.docker.ListVolumes(ctx); err == nil {
		for _, v := range vols {
			existingVolumes[v] = struct{}{}
		}
	} else {
		return apperr.Wrap("planner.Apply", apperr.External, err, "list volumes")
	}
	for name := range cfg.Volumes {
		if _, ok := existingVolumes[name]; !ok {
			if err := p.docker.CreateVolume(ctx, name, labels); err != nil {
				return apperr.Wrap("planner.Apply", apperr.External, err, "create volume %s", name)
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
				if err := p.docker.RemovePathsFromVolume(ctx, a.TargetVolume, a.TargetPath, diff.ToDelete); err != nil {
					return apperr.Wrap("planner.Apply", apperr.External, err, "delete files for fileset %s", n)
				}
			}
			// Write index last (not part of tree)
			jsonStr, err := local.ToJSON()
			if err != nil {
				return apperr.Wrap("planner.Apply", apperr.Internal, err, "encode index for %s", n)
			}
			if err := p.docker.WriteFileToVolume(ctx, a.TargetVolume, a.TargetPath, filesets.IndexFileName, jsonStr); err != nil {
				return apperr.Wrap("planner.Apply", apperr.External, err, "write index for fileset %s", n)
			}

			// Restart services if configured for this fileset group
			if len(a.RestartServices) > 0 {
				// Discover running compose services scoped by identifier (client carries identifier filter)
				items, _ := p.docker.ListComposeContainersAll(ctx)
				for _, svc := range a.RestartServices {
					if svc == "" {
						continue
					}
					found := false
					for _, it := range items {
						if it.Service == svc {
							found = true
							fmt.Printf("restarting service %s...\n", svc)
							if err := p.docker.RestartContainer(ctx, it.Name); err != nil {
								return apperr.Wrap("planner.Apply", apperr.External, err, "restart service %s", svc)
							}
						}
					}
					if !found {
						fmt.Printf("Warning: %s not found.\n", svc)
					}
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
			if err := p.docker.CreateNetwork(ctx, name, labels); err != nil {
				return apperr.Wrap("planner.Apply", apperr.External, err, "create network %s", name)
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
		if doc, err := p.docker.ComposeConfigFull(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline); err == nil {
			for s := range doc.Services {
				plannedServices = append(plannedServices, s)
			}
			sort.Strings(plannedServices)
		}
		if len(plannedServices) == 0 {
			if names, err := p.docker.ComposeConfigServices(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline); err == nil && len(names) > 0 {
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
		if _, err := p.docker.ComposeUp(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, inline); err != nil {
			return apperr.Wrap("planner.Apply", apperr.External, err, "compose up %s", appName)
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
	return nil
}

// Prune removes unmanaged resources labeled with the identifier.
// It deletes volumes, networks, and containers that are labeled but not present in cfg.
func (p *Planner) Prune(ctx context.Context, cfg config.Config) error {
	if p.docker == nil {
		return apperr.New("planner.Prune", apperr.Precondition, "docker client not configured")
	}
	// Desired services set across all applications
	desiredServices := map[string]struct{}{}
	for _, app := range cfg.Applications {
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
		if doc, err := p.docker.ComposeConfigFull(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline); err == nil {
			for s := range doc.Services {
				desiredServices[s] = struct{}{}
			}
		}
	}
	// Remove labeled containers not in desired set
	if all, err := p.docker.ListComposeContainersAll(ctx); err == nil {
		for _, it := range all {
			if _, want := desiredServices[it.Service]; !want {
				_ = p.docker.RemoveContainer(ctx, it.Name, true)
			}
		}
	}
	// Remove labeled volumes not in cfg
	if vols, err := p.docker.ListVolumes(ctx); err == nil {
		for _, v := range vols {
			if _, want := cfg.Volumes[v]; !want {
				_ = p.docker.RemoveVolume(ctx, v)
			}
		}
	}
	// Remove labeled networks not in cfg
	if nets, err := p.docker.ListNetworks(ctx); err == nil {
		for _, n := range nets {
			if _, want := cfg.Networks[n]; !want {
				_ = p.docker.RemoveNetwork(ctx, n)
			}
		}
	}
	return nil
}
