package data

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

// ComposeClient captures the docker compose operations required by the dashboard loader.
type ComposeClient interface {
	ComposeConfigFull(ctx context.Context, workingDir string, files []string, profiles []string, envFiles []string, inline []string) (dockercli.ComposeConfigDoc, error)
}

// Loader prepares dashboard-ready data derived from the manifest and compose files.
type Loader struct {
	cfg    *manifest.Config
	docker ComposeClient
}

// NewLoader creates a new dashboard data loader. The docker client may be nil for tests.
func NewLoader(cfg *manifest.Config, docker ComposeClient) (*Loader, error) {
	if cfg == nil {
		return nil, apperr.New("dashboard.data.NewLoader", apperr.Internal, "nil manifest config")
	}
	return &Loader{cfg: cfg, docker: docker}, nil
}

// StackSummary represents the information needed to populate the stacks column.
type StackSummary struct {
	Name     string
	Services []ServiceSummary
}

// ServiceSummary holds the compose service metadata displayed in the dashboard.
type ServiceSummary struct {
	Service       string
	ContainerName string
	Image         string
	Networks      []string
	Volumes       []string
}

// StackSummaries resolves compose metadata for each manifest stack.
func (l *Loader) StackSummaries(ctx context.Context) ([]StackSummary, error) {
	if l.cfg == nil {
		return nil, apperr.New("dashboard.data.StackSummaries", apperr.Internal, "manifest config not initialized")
	}
	if l.docker == nil {
		return nil, apperr.New("dashboard.data.StackSummaries", apperr.Internal, "docker client unavailable")
	}

	summaries := make([]StackSummary, 0, len(l.cfg.Stacks))
	stackNames := make([]string, 0, len(l.cfg.Stacks))
	for name := range l.cfg.Stacks {
		stackNames = append(stackNames, name)
	}
	sort.Strings(stackNames)

	for _, name := range stackNames {
		stack := l.cfg.Stacks[name]
		services, err := l.loadServices(ctx, name, stack)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, StackSummary{Name: name, Services: services})
	}

	return summaries, nil
}

func (l *Loader) loadServices(ctx context.Context, stackName string, stack manifest.Stack) ([]ServiceSummary, error) {
	workingDir := stack.Root
	if workingDir == "" {
		workingDir = l.cfg.BaseDir
	} else if !filepath.IsAbs(workingDir) {
		workingDir = filepath.Join(l.cfg.BaseDir, workingDir)
	}

	files := normalizePaths(workingDir, stack.Files)
	envFiles := normalizePaths(workingDir, stack.EnvFile)
	inline := append([]string(nil), stack.EnvInline...)

	doc, err := l.docker.ComposeConfigFull(ctx, workingDir, files, stack.Profiles, envFiles, inline)
	if err != nil {
		return nil, apperr.Wrap("dashboard.data.loadContainers", apperr.Internal, err, "failed for stack: %s", stackName)
	}

	services := make([]ServiceSummary, 0, len(doc.Services))
	for serviceName, svc := range doc.Services {
		image := strings.TrimSpace(svc.Image)
		if image == "" {
			image = "(no image)"
		}
		containerName := strings.TrimSpace(svc.ContainerName)
		networks := make([]string, 0, len(svc.Networks))
		for _, n := range svc.Networks {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			networks = append(networks, n)
		}
		volumes := make([]string, 0, len(svc.Volumes))
		for _, v := range svc.Volumes {
			if strings.TrimSpace(v.Type) != "volume" {
				continue
			}
			source := strings.TrimSpace(v.Source)
			if source == "" {
				continue
			}
			volumes = append(volumes, source)
		}
		services = append(services, ServiceSummary{Service: serviceName, ContainerName: containerName, Image: image, Networks: networks, Volumes: volumes})
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].Service < services[j].Service
	})
	return services, nil
}

func normalizePaths(base string, rels []string) []string {
	if len(rels) == 0 {
		return nil
	}
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		if rel == "" {
			continue
		}
		if filepath.IsAbs(rel) {
			out = append(out, rel)
			continue
		}
		out = append(out, filepath.Join(base, rel))
	}
	return out
}
