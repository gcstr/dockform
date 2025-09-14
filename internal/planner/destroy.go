package planner

import (
	"context"
	"fmt"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
)

// BuildDestroyPlan creates a plan to destroy all managed resources.
// Unlike BuildPlan, this discovers all labeled resources regardless of configuration.
func (p *Planner) BuildDestroyPlan(ctx context.Context, cfg manifest.Config) (*Plan, error) {
	if p.docker == nil {
		return nil, apperr.New("planner.BuildDestroyPlan", apperr.Precondition, "docker client not configured")
	}

	rp := &ResourcePlan{
		Applications: make(map[string][]Resource),
		Filesets:     make(map[string][]Resource),
	}

	// Discover all labeled containers and group by application
	containers, err := p.docker.ListComposeContainersAll(ctx)
	if err != nil {
		return nil, apperr.Wrap("planner.BuildDestroyPlan", apperr.External, err, "list containers")
	}

	// Group containers by project (application)
	appContainers := make(map[string][]Resource)
	for _, container := range containers {
		// Group by project name as application
		if container.Project != "" {
			res := NewResource(ResourceService, container.Service, ActionDelete, "will be destroyed")
			if _, exists := appContainers[container.Project]; !exists {
				appContainers[container.Project] = []Resource{}
			}
			appContainers[container.Project] = append(appContainers[container.Project], res)
		} else {
			// Orphaned container without project
			res := NewResource(ResourceContainer, container.Name, ActionDelete, "will be destroyed")
			rp.Containers = append(rp.Containers, res)
		}
	}
	
	// Add grouped containers to applications
	for app, resources := range appContainers {
		rp.Applications[app] = resources
	}

	// Discover all labeled networks
	networks, err := p.docker.ListNetworks(ctx)
	if err != nil {
		return nil, apperr.Wrap("planner.BuildDestroyPlan", apperr.External, err, "list networks")
	}
	for _, network := range networks {
		res := NewResource(ResourceNetwork, network, ActionDelete, "will be destroyed")
		rp.Networks = append(rp.Networks, res)
	}

	// Discover all labeled volumes and check for filesets
	volumes, err := p.docker.ListVolumes(ctx)
	if err != nil {
		return nil, apperr.Wrap("planner.BuildDestroyPlan", apperr.External, err, "list volumes")
	}
	
	// Map volumes to filesets if possible
	volumeToFileset := make(map[string]string)
	for fsName, fs := range cfg.Filesets {
		volumeToFileset[fs.TargetVolume] = fsName
	}
	
	for _, volume := range volumes {
		// Check if this volume is associated with a fileset
		if filesetName, hasFileset := volumeToFileset[volume]; hasFileset {
			// Add to filesets section
			if _, exists := rp.Filesets[filesetName]; !exists {
				rp.Filesets[filesetName] = []Resource{}
			}
			
			// Get fileset config for details
			fsConfig := cfg.Filesets[filesetName]
			details := fmt.Sprintf("volume %s at %s will be destroyed", volume, fsConfig.TargetPath)
			res := NewResource(ResourceFile, "", ActionDelete, details)
			rp.Filesets[filesetName] = append(rp.Filesets[filesetName], res)
		} else {
			// Regular volume not associated with a fileset
			res := NewResource(ResourceVolume, volume, ActionDelete, "will be destroyed")
			rp.Volumes = append(rp.Volumes, res)
		}
	}

	return &Plan{Resources: rp}, nil
}

// Destroy executes the destruction of all managed resources.
func (p *Planner) Destroy(ctx context.Context, cfg manifest.Config) error {
	if p.docker == nil {
		return apperr.New("planner.Destroy", apperr.Precondition, "docker client not configured")
	}

	// Build the destroy plan to get all resources
	plan, err := p.BuildDestroyPlan(ctx, cfg)
	if err != nil {
		return err
	}

	if plan.Resources == nil {
		return nil
	}

	rp := plan.Resources
	
	// Calculate total operations for progress tracking
	total := 0
	for _, services := range rp.Applications {
		total += len(services)
	}
	total += len(rp.Containers)
	total += len(rp.Networks) 
	total += len(rp.Volumes)
	for _, files := range rp.Filesets {
		// Count filesets as single operations (they represent volumes)
		if len(files) > 0 {
			total++
		}
	}
	
	if p.prog != nil {
		p.prog.Start(total)
	}

	// Step 1: Remove containers (grouped by application)
	for appName, services := range rp.Applications {
		for _, svc := range services {
			if p.prog != nil {
				p.prog.SetAction(fmt.Sprintf("removing service %s/%s", appName, svc.Name))
			}
			
			// Find and remove actual containers for this service
			containers, _ := p.docker.ListComposeContainersAll(ctx)
			for _, c := range containers {
				if c.Project == appName && c.Service == svc.Name {
					_ = p.docker.RemoveContainer(ctx, c.Name, true)
				}
			}
			
			if p.prog != nil {
				p.prog.Increment()
			}
		}
	}
	
	// Remove orphaned containers
	for _, container := range rp.Containers {
		if p.prog != nil {
			p.prog.SetAction(fmt.Sprintf("removing container %s", container.Name))
		}
		_ = p.docker.RemoveContainer(ctx, container.Name, true)
		if p.prog != nil {
			p.prog.Increment()
		}
	}

	// Step 2: Remove networks
	for _, network := range rp.Networks {
		if p.prog != nil {
			p.prog.SetAction(fmt.Sprintf("removing network %s", network.Name))
		}
		_ = p.docker.RemoveNetwork(ctx, network.Name)
		if p.prog != nil {
			p.prog.Increment()
		}
	}

	// Step 3: Remove volumes (including those from filesets)
	// First, volumes associated with filesets
	volumesRemoved := make(map[string]bool)
	for filesetName, files := range rp.Filesets {
		if len(files) > 0 {
			// Find the volume for this fileset
			if fs, exists := cfg.Filesets[filesetName]; exists {
				if p.prog != nil {
					p.prog.SetAction(fmt.Sprintf("removing fileset %s (volume %s)", filesetName, fs.TargetVolume))
				}
				_ = p.docker.RemoveVolume(ctx, fs.TargetVolume)
				volumesRemoved[fs.TargetVolume] = true
				if p.prog != nil {
					p.prog.Increment()
				}
			}
		}
	}
	
	// Then, standalone volumes
	for _, volume := range rp.Volumes {
		if !volumesRemoved[volume.Name] {
			if p.prog != nil {
				p.prog.SetAction(fmt.Sprintf("removing volume %s", volume.Name))
			}
			_ = p.docker.RemoveVolume(ctx, volume.Name)
			if p.prog != nil {
				p.prog.Increment()
			}
		}
	}

	return nil
}