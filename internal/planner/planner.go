package planner

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

			// Desired config (images, ports) from compose config
			doc, derr := p.docker.ComposeConfigFull(ctx, app.Root, app.Files, app.Profiles, app.EnvFile)
			plannedServices := sortedKeys(doc.Services)
			// Fallback to services list if no services parsed or error occurred
			if derr != nil || len(plannedServices) == 0 {
				if names, err := p.docker.ComposeConfigServices(ctx, app.Root, app.Files, app.Profiles, app.EnvFile); err == nil && len(names) > 0 {
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
			if items, err := p.docker.ComposePs(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj); err == nil {
				for _, it := range items {
					running[it.Service] = it
				}
			}

			for _, s := range plannedServices {
				if it, ok := running[s]; ok {
					// Compare image and ports if config details available
					if svc, has := doc.Services[s]; has {
						desiredImage := svc.Image
						if desiredImage != "" && it.Image != "" && it.Image != desiredImage {
							lines = append(lines, ui.Line(ui.Change, "service %s/%s image: %s -> %s", appName, s, it.Image, desiredImage))
						}
						portDelta := comparePortsCoerce(svc.Ports, it.Publishers)
						if portDelta != "" {
							lines = append(lines, ui.Line(ui.Change, "service %s/%s ports: %s", appName, s, portDelta))
						}
						if desiredImage == it.Image && portDelta == "" {
							lines = append(lines, ui.Line(ui.Noop, "service %s/%s running", appName, s))
						}
					} else {
						// No config details; just mark running
						lines = append(lines, ui.Line(ui.Noop, "service %s/%s running", appName, s))
					}
				} else {
					lines = append(lines, ui.Line(ui.Add, "service %s/%s will be started", appName, s))
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

func comparePorts(desired []dockercli.ComposePort, running []dockercli.ComposePublisher) string {
	// Build sets of "published:target/proto"
	dset := map[string]struct{}{}
	for _, p := range desired {
		key := fmt.Sprintf("%d:%d/%s", p.Published, p.Target, normalizeProto(p.Protocol))
		dset[key] = struct{}{}
	}
	rset := map[string]struct{}{}
	for _, p := range running {
		key := fmt.Sprintf("%d:%d/%s", p.PublishedPort, p.TargetPort, normalizeProto(p.Protocol))
		rset[key] = struct{}{}
	}
	// Compare sets
	var adds, removes []string
	for k := range dset {
		if _, ok := rset[k]; !ok {
			adds = append(adds, "+"+k)
		}
	}
	for k := range rset {
		if _, ok := dset[k]; !ok {
			removes = append(removes, "-"+k)
		}
	}
	if len(adds) == 0 && len(removes) == 0 {
		return ""
	}
	sort.Strings(adds)
	sort.Strings(removes)
	return strings.Join(append(removes, adds...), ", ")
}

func comparePortsCoerce(desired []dockercli.ComposePort, running []dockercli.ComposePublisher) string {
	coerced := make([]dockercli.ComposePort, 0, len(desired))
	for _, p := range desired {
		pub := 0
		switch v := p.Published.(type) {
		case int:
			pub = v
		case int64:
			pub = int(v)
		case string:
			fmt.Sscanf(v, "%d", &pub)
		}
		coerced = append(coerced, dockercli.ComposePort{Target: p.Target, Published: pub, Protocol: p.Protocol})
	}
	return comparePorts(coerced, running)
}

func normalizeProto(p string) string {
	if p == "" {
		return "tcp"
	}
	return strings.ToLower(p)
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
