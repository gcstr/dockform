package planner

import (
	"context"
	"fmt"
	"sync"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
)

// BuildDestroyPlan creates a plan to destroy all managed resources.
// Unlike BuildPlan, this discovers all labeled resources regardless of configuration.
func (p *Planner) BuildDestroyPlan(ctx context.Context, cfg manifest.Config) (*Plan, error) {
	if p.docker == nil && p.factory == nil {
		return nil, apperr.New("planner.BuildDestroyPlan", apperr.Precondition, "docker client not configured")
	}

	rp := &ResourcePlan{
		Stacks:   make(map[string][]Resource),
		Filesets: make(map[string][]Resource),
	}

	allFilesets := cfg.GetAllFilesets()
	volumeToFileset := make(map[string]string)
	for fsName, fs := range allFilesets {
		volumeToFileset[fs.TargetVolume] = fsName
	}

	var mu sync.Mutex

	err := p.ExecuteAcrossContexts(ctx, &cfg, func(ctx context.Context, contextName string) error {
		client := p.getClientForContext(contextName, &cfg)
		if client == nil {
			return apperr.New("planner.BuildDestroyPlan", apperr.Precondition, "docker client not available for context %s", contextName)
		}

		localRP, err := p.buildDestroyPlanForContext(ctx, client, contextName, allFilesets, volumeToFileset)
		if err != nil {
			return err
		}

		mu.Lock()
		defer mu.Unlock()
		mergeResourcePlan(rp, localRP)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &Plan{Resources: rp}, nil
}

// buildDestroyPlanForContext discovers labeled resources on a single context.
func (p *Planner) buildDestroyPlanForContext(ctx context.Context, client DockerClient, contextName string, allFilesets map[string]manifest.FilesetSpec, volumeToFileset map[string]string) (*ResourcePlan, error) {
	rp := &ResourcePlan{
		Stacks:   make(map[string][]Resource),
		Filesets: make(map[string][]Resource),
	}

	// Discover all labeled containers and group by stack
	containers, err := client.ListComposeContainersAll(ctx)
	if err != nil {
		return nil, apperr.Wrap("planner.BuildDestroyPlan", apperr.External, err, "context %s: list containers", contextName)
	}

	stackServices := make(map[string]map[string]struct{})
	for _, container := range containers {
		if container.Project != "" {
			if stackServices[container.Project] == nil {
				stackServices[container.Project] = make(map[string]struct{})
			}
			stackServices[container.Project][container.Service] = struct{}{}
		} else {
			res := NewResource(ResourceContainer, container.Name, ActionDelete, "will be destroyed")
			rp.Containers = append(rp.Containers, res)
		}
	}

	for stack, services := range stackServices {
		key := manifest.MakeStackKey(contextName, stack)
		rp.Stacks[key] = []Resource{}
		for svc := range services {
			res := NewResource(ResourceService, svc, ActionDelete, "will be destroyed")
			rp.Stacks[key] = append(rp.Stacks[key], res)
		}
	}

	// Discover all labeled networks
	networks, err := client.ListNetworks(ctx)
	if err != nil {
		return nil, apperr.Wrap("planner.BuildDestroyPlan", apperr.External, err, "context %s: list networks", contextName)
	}
	for _, network := range networks {
		res := NewResource(ResourceNetwork, network, ActionDelete, "will be destroyed")
		rp.Networks = append(rp.Networks, res)
	}

	// Discover all labeled volumes
	volumes, err := client.ListVolumes(ctx)
	if err != nil {
		return nil, apperr.Wrap("planner.BuildDestroyPlan", apperr.External, err, "context %s: list volumes", contextName)
	}

	for _, volume := range volumes {
		if filesetName, hasFileset := volumeToFileset[volume]; hasFileset {
			if _, exists := rp.Filesets[filesetName]; !exists {
				rp.Filesets[filesetName] = []Resource{}
			}
			fsConfig := allFilesets[filesetName]
			details := fmt.Sprintf("volume %s at %s will be destroyed", volume, fsConfig.TargetPath)
			res := NewResource(ResourceFile, "", ActionDelete, details)
			rp.Filesets[filesetName] = append(rp.Filesets[filesetName], res)
		} else {
			res := NewResource(ResourceVolume, volume, ActionDelete, "will be destroyed")
			rp.Volumes = append(rp.Volumes, res)
		}
	}

	return rp, nil
}

// mergeResourcePlan merges src into dst.
func mergeResourcePlan(dst, src *ResourcePlan) {
	dst.Volumes = append(dst.Volumes, src.Volumes...)
	dst.Networks = append(dst.Networks, src.Networks...)
	dst.Containers = append(dst.Containers, src.Containers...)
	for k, v := range src.Stacks {
		dst.Stacks[k] = append(dst.Stacks[k], v...)
	}
	for k, v := range src.Filesets {
		dst.Filesets[k] = append(dst.Filesets[k], v...)
	}
}

// Destroy executes the destruction of all managed resources.
func (p *Planner) Destroy(ctx context.Context, cfg manifest.Config) error {
	if p.docker == nil && p.factory == nil {
		return apperr.New("planner.Destroy", apperr.Precondition, "docker client not configured")
	}

	allFilesets := cfg.GetAllFilesets()

	return p.ExecuteAcrossContexts(ctx, &cfg, func(ctx context.Context, contextName string) error {
		client := p.getClientForContext(contextName, &cfg)
		if client == nil {
			return apperr.New("planner.Destroy", apperr.Precondition, "docker client not available for context %s", contextName)
		}

		return p.destroyContext(ctx, client, contextName, allFilesets)
	})
}

// destroyContext executes destruction for a single context.
// Errors during resource removal are logged but do not stop the destruction process
// to ensure best-effort cleanup of all resources.
func (p *Planner) destroyContext(ctx context.Context, client DockerClient, contextName string, allFilesets map[string]manifest.FilesetSpec) error {
	log := logger.FromContext(ctx).With("component", "planner", "action", "destroy", "context", contextName)

	// Step 1: Remove containers
	allContainers, err := client.ListComposeContainersAll(ctx)
	if err != nil {
		log.Warn("destroy_list_containers_failed", "error", err.Error())
	}
	byProjSvc := make(map[string]map[string][]string)
	for _, it := range allContainers {
		if it.Project == "" {
			// Orphaned container
			if p.spinner != nil {
				p.spinner.SetLabel(fmt.Sprintf("removing container %s on %s", it.Name, contextName))
			}
			if err := client.RemoveContainer(ctx, it.Name, true); err != nil {
				log.Warn("destroy_remove_container_failed", "container", it.Name, "error", err.Error())
			}
			continue
		}
		if byProjSvc[it.Project] == nil {
			byProjSvc[it.Project] = make(map[string][]string)
		}
		byProjSvc[it.Project][it.Service] = append(byProjSvc[it.Project][it.Service], it.Name)
	}

	for stackName, services := range byProjSvc {
		for svcName, containerNames := range services {
			if p.spinner != nil {
				p.spinner.SetLabel(fmt.Sprintf("removing service %s/%s on %s", stackName, svcName, contextName))
			}
			for _, name := range containerNames {
				if err := client.RemoveContainer(ctx, name, true); err != nil {
					log.Warn("destroy_remove_container_failed", "container", name, "stack", stackName, "service", svcName, "error", err.Error())
				}
			}
		}
	}

	// Step 2: Remove networks
	networks, err := client.ListNetworks(ctx)
	if err != nil {
		log.Warn("destroy_list_networks_failed", "error", err.Error())
	}
	for _, network := range networks {
		if p.spinner != nil {
			p.spinner.SetLabel(fmt.Sprintf("removing network %s on %s", network, contextName))
		}
		if err := client.RemoveNetwork(ctx, network); err != nil {
			log.Warn("destroy_remove_network_failed", "network", network, "error", err.Error())
		}
	}

	// Step 3: Remove volumes
	volumes, err := client.ListVolumes(ctx)
	if err != nil {
		log.Warn("destroy_list_volumes_failed", "error", err.Error())
	}
	for _, volume := range volumes {
		if p.spinner != nil {
			p.spinner.SetLabel(fmt.Sprintf("removing volume %s on %s", volume, contextName))
		}
		if err := client.RemoveVolume(ctx, volume); err != nil {
			log.Warn("destroy_remove_volume_failed", "volume", volume, "error", err.Error())
		}
	}

	return nil
}
