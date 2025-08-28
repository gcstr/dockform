package planner

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/config"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/secrets"
	"github.com/gcstr/dockform/internal/ui"
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
			for _, s := range app.SopsSecrets {
				pth := s.Path
				if pth != "" && !filepath.IsAbs(pth) {
					pth = filepath.Join(app.Root, pth)
				}
				if pairs, err := secrets.DecryptAndParse(ctx, pth, s.Format, ageKeyFile); err == nil {
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
						keys = append(keys, "io.dockform/"+cfg.Docker.Identifier)
					}
					labels, _ := p.docker.InspectContainerLabels(ctx, it.Name, keys)
					if cfg.Docker.Identifier != "" {
						if _, ok := labels["io.dockform/"+cfg.Docker.Identifier]; !ok {
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

	if len(lines) == 0 {
		lines = append(lines, ui.Line(ui.Noop, "nothing to do"))
	}
	return &Plan{Lines: lines}, nil
}

func (p *Plan) String() string {
	out := ""
	for i, l := range p.Lines {
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
		labels["io.dockform/"+identifier] = "1"
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

	// Sync assets into volumes (deterministic order)
	if len(cfg.Assets) > 0 {
		assetNames := make([]string, 0, len(cfg.Assets))
		for n := range cfg.Assets {
			assetNames = append(assetNames, n)
		}
		sort.Strings(assetNames)
		for _, n := range assetNames {
			a := cfg.Assets[n]
			if a.SourceAbs == "" {
				return apperr.New("planner.Apply", apperr.InvalidInput, "asset %s: resolved source path is empty", n)
			}
			if err := p.docker.SyncDirToVolume(ctx, a.TargetVolume, a.TargetPath, a.SourceAbs); err != nil {
				return apperr.Wrap("planner.Apply", apperr.External, err, "sync asset %s to volume %s at %s", n, a.TargetVolume, a.TargetPath)
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

	// Compose up each application
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
		for _, s := range app.SopsSecrets {
			pth := s.Path
			if pth != "" && !filepath.IsAbs(pth) {
				pth = filepath.Join(app.Root, pth)
			}
			if pairs, err := secrets.DecryptAndParse(ctx, pth, s.Format, ageKeyFile); err == nil {
				inline = append(inline, pairs...)
			}
		}
		if _, err := p.docker.ComposeUp(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, inline); err != nil {
			return apperr.Wrap("planner.Apply", apperr.External, err, "compose up %s", appName)
		}
		// Label running containers for this app with identifier (best-effort)
		if identifier != "" {
			if items, err := p.docker.ComposePs(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, inline); err == nil {
				for _, it := range items {
					_ = p.docker.UpdateContainerLabels(ctx, it.Name, map[string]string{"io.dockform/" + identifier: "1"})
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
		for _, s := range app.SopsSecrets {
			pth := s.Path
			if pth != "" && !filepath.IsAbs(pth) {
				pth = filepath.Join(app.Root, pth)
			}
			if pairs, err := secrets.DecryptAndParse(ctx, pth, s.Format, ageKeyFile); err == nil {
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
