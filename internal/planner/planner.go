package planner

import (
	"context"
	"fmt"
	"sort"

	"github.com/gcstr/dockform/internal/config"
	"github.com/gcstr/dockform/internal/dockercli"
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

	// Applications: compose planned vs running diff
	if len(cfg.Applications) == 0 {
		lines = append(lines, ui.Line(ui.Noop, "no applications defined"))
	} else {
		appNames := make([]string, 0, len(cfg.Applications))
		for name := range cfg.Applications {
			appNames = append(appNames, name)
		}
		sort.Strings(appNames)
		for _, appName := range appNames {
			app := cfg.Applications[appName]
			if p.docker == nil {
				lines = append(lines, ui.Line(ui.Noop, "application %s planned (services diff TBD)", appName))
				continue
			}

			// Desired config (images) from compose config
			doc, _ := p.docker.ComposeConfigFull(ctx, app.Root, app.Files, app.Profiles, app.EnvFile)
			plannedServices := sortedKeys(doc.Services)

			running := map[string]dockercli.ComposePsItem{}
			proj := ""
			if app.Project != nil {
				proj = app.Project.Name
			}
			if items, err := p.docker.ComposePs(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj); err == nil {
				for _, it := range items {
					running[it.Service] = it
				}
			}

			for _, s := range plannedServices {
				if it, ok := running[s]; ok {
					// Compare image
					desiredImage := doc.Services[s].Image
					if desiredImage != "" && it.Image != "" && it.Image != desiredImage {
						lines = append(lines, ui.Line(ui.Change, "service %s/%s image: %s -> %s", appName, s, it.Image, desiredImage))
					} else {
						lines = append(lines, ui.Line(ui.Noop, "service %s/%s running", appName, s))
					}
				} else {
					lines = append(lines, ui.Line(ui.Add, "service %s/%s will be started", appName, s))
				}
			}
		}
	}

	// Unmanaged (prunable) containers: those with compose labels but not in desired apps/projects
	if p.docker != nil {
		managedProjects := map[string]struct{}{}
		for _, app := range cfg.Applications {
			if app.Project != nil && app.Project.Name != "" {
				managedProjects[app.Project.Name] = struct{}{}
			}
		}
		if items, err := p.docker.ListComposeContainersAll(ctx); err == nil {
			for _, it := range items {
				if _, ok := managedProjects[it.Project]; !ok {
					lines = append(lines, ui.Line(ui.Remove, "container %s unmanaged (project %s) - will be removed with --prune", it.Name, it.Project))
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

// Apply creates missing top-level resources with labels and performs compose up, labeling containers with identifier.
func (p *Planner) Apply(ctx context.Context, cfg config.Config) error {
	if p.docker == nil {
		return fmt.Errorf("docker client not configured")
	}
	identifier := cfg.Docker.Identifier
	labels := map[string]string{}
	if identifier != "" {
		labels["dockform.identifier"] = identifier
	}

	// Ensure volumes exist
	existingVolumes := map[string]struct{}{}
	if vols, err := p.docker.ListVolumes(ctx); err == nil {
		for _, v := range vols {
			existingVolumes[v] = struct{}{}
		}
	} else {
		return fmt.Errorf("list volumes: %w", err)
	}
	for name := range cfg.Volumes {
		if _, ok := existingVolumes[name]; !ok {
			if err := p.docker.CreateVolume(ctx, name, labels); err != nil {
				return fmt.Errorf("create volume %s: %w", name, err)
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
		return fmt.Errorf("list networks: %w", err)
	}
	for name := range cfg.Networks {
		if _, ok := existingNetworks[name]; !ok {
			if err := p.docker.CreateNetwork(ctx, name, labels); err != nil {
				return fmt.Errorf("create network %s: %w", name, err)
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
		if _, err := p.docker.ComposeUp(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj); err != nil {
			return fmt.Errorf("compose up %s: %w", appName, err)
		}
		// Label running containers for this app with identifier (best-effort)
		if identifier != "" {
			if items, err := p.docker.ComposePs(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj); err == nil {
				for _, it := range items {
					_ = p.docker.UpdateContainerLabels(ctx, it.Name, map[string]string{"dockform.identifier": identifier})
				}
			}
		}
	}
	return nil
}
