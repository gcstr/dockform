package planner

import (
	"context"
	"path/filepath"
	"sort"
	"sync"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/secrets"
)

// ServiceState represents the current state of a service relative to its desired configuration.
type ServiceState int

const (
	// ServiceMissing indicates the service is not running
	ServiceMissing ServiceState = iota
	// ServiceRunning indicates the service is running and up-to-date
	ServiceRunning
	// ServiceDrifted indicates the service is running but configuration has drifted
	ServiceDrifted
	// ServiceIdentifierMismatch indicates the service is running but has wrong identifier label
	ServiceIdentifierMismatch
)

// ServiceInfo contains information about a service's desired and actual state.
type ServiceInfo struct {
	Name         string
	AppName      string
	State        ServiceState
	DesiredHash  string
	RunningHash  string
	Container    *dockercli.ComposePsItem // nil if not running
}

// ServiceStateDetector handles detection of service state changes.
type ServiceStateDetector struct {
	docker   DockerClient
	parallel bool
}

// NewServiceStateDetector creates a new service state detector.
func NewServiceStateDetector(docker DockerClient) *ServiceStateDetector {
	return &ServiceStateDetector{docker: docker}
}

// WithParallel enables or disables parallel processing for service state detection.
func (d *ServiceStateDetector) WithParallel(enabled bool) *ServiceStateDetector {
	d.parallel = enabled
	return d
}

// GetPlannedServices returns the list of services defined in the application's compose files.
func (d *ServiceStateDetector) GetPlannedServices(ctx context.Context, app manifest.Application, inline []string) ([]string, error) {
	if d.docker == nil {
		return nil, nil
	}
	
	// First try to get services from compose config
	doc, err := d.docker.ComposeConfigFull(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline)
	if err != nil {
		return nil, err
	}
	
	services := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		services = append(services, name)
	}
	sort.Strings(services)
	
	// If no services found, try the services command as fallback
	if len(services) == 0 {
		services, err = d.docker.ComposeConfigServices(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline)
		if err != nil {
			return nil, err
		}
		if len(services) > 0 {
			sort.Strings(services)
		}
	}
	
	return services, nil
}

// BuildInlineEnv constructs the inline environment variables for an application, including SOPS secrets.
func (d *ServiceStateDetector) BuildInlineEnv(ctx context.Context, app manifest.Application, sopsConfig *manifest.SopsConfig) []string {
	inline := append([]string(nil), app.EnvInline...)
	
	ageKeyFile := ""
	if sopsConfig != nil && sopsConfig.Age != nil {
		ageKeyFile = sopsConfig.Age.KeyFile
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
	
	return inline
}

// GetRunningServices returns a map of currently running services for the application.
func (d *ServiceStateDetector) GetRunningServices(ctx context.Context, app manifest.Application, inline []string) (map[string]dockercli.ComposePsItem, error) {
	running := map[string]dockercli.ComposePsItem{}
	
	if d.docker == nil {
		return running, nil
	}
	
	proj := ""
	if app.Project != nil {
		proj = app.Project.Name
	}
	
	items, err := d.docker.ComposePs(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, inline)
	if err != nil {
		// Treat compose ps errors as "no running services" rather than hard error
		return running, nil
	}
	
	for _, item := range items {
		running[item.Service] = item
	}
	
	return running, nil
}

// DetectServiceState determines the state of a single service.
func (d *ServiceStateDetector) DetectServiceState(ctx context.Context, serviceName, appName string, app manifest.Application, identifier string, inline []string, running map[string]dockercli.ComposePsItem) (ServiceInfo, error) {
	info := ServiceInfo{
		Name:    serviceName,
		AppName: appName,
		State:   ServiceMissing,
	}
	
	// Check if service is running first, regardless of docker client availability
	container, isRunning := running[serviceName]
	if isRunning {
		info.Container = &container
		info.State = ServiceRunning // Assume running until we find issues
	}
	
	if d.docker == nil {
		return info, nil // Can't do deeper inspection without docker client
	}
	
	// Get the project name
	proj := ""
	if app.Project != nil {
		proj = app.Project.Name
	}
	
	// Compute desired hash
	desiredHash, hashErr := d.docker.ComposeConfigHash(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, proj, serviceName, identifier, inline)
	info.DesiredHash = desiredHash
	
	// If service is not running, we already set that up above
	if !isRunning {
		return info, nil
	}
	
	// Check labels to determine actual state
	keys := []string{"com.docker.compose.config-hash"}
	if identifier != "" {
		keys = append(keys, "io.dockform.identifier")
	}
	
	labels, err := d.docker.InspectContainerLabels(ctx, info.Container.Name, keys)
	if err != nil {
		// If we can't inspect labels, assume it needs to be reconciled
		info.State = ServiceDrifted
		return info, nil
	}
	
	// Check identifier mismatch first (higher priority)
	if identifier != "" {
		if v, ok := labels["io.dockform.identifier"]; !ok || v != identifier {
			info.State = ServiceIdentifierMismatch
			return info, nil
		}
	}
	
	// Check hash drift if we have a desired hash
	if hashErr == nil && desiredHash != "" {
		runningHash := labels["com.docker.compose.config-hash"]
		info.RunningHash = runningHash
		if runningHash == "" || runningHash != desiredHash {
			info.State = ServiceDrifted
			return info, nil
		}
	}
	
	return info, nil
}

// DetectAllServicesState analyzes the state of all services in an application.
func (d *ServiceStateDetector) DetectAllServicesState(ctx context.Context, appName string, app manifest.Application, identifier string, sopsConfig *manifest.SopsConfig) ([]ServiceInfo, error) {
	// Build inline environment
	inline := d.BuildInlineEnv(ctx, app, sopsConfig)
	
	// Get planned services
	plannedServices, err := d.GetPlannedServices(ctx, app, inline)
	if err != nil {
		return nil, apperr.Wrap("servicestate.DetectAllServicesState", apperr.External, err, "failed to get planned services for application %s", appName)
	}
	
	if len(plannedServices) == 0 {
		return nil, nil
	}
	
	// Get running services
	running, err := d.GetRunningServices(ctx, app, inline)
	if err != nil {
		return nil, apperr.Wrap("servicestate.DetectAllServicesState", apperr.External, err, "failed to get running services for application %s", appName)
	}
	
	// Choose parallel or sequential processing based on configuration
	if d.parallel {
		return d.detectAllServicesStateParallel(ctx, appName, app, identifier, inline, running, plannedServices)
	}
	return d.detectAllServicesStateSequential(ctx, appName, app, identifier, inline, running, plannedServices)
}

// detectAllServicesStateSequential processes services one by one (original implementation)
func (d *ServiceStateDetector) detectAllServicesStateSequential(ctx context.Context, appName string, app manifest.Application, identifier string, inline []string, running map[string]dockercli.ComposePsItem, plannedServices []string) ([]ServiceInfo, error) {
	// Analyze each planned service
	var results []ServiceInfo
	for _, serviceName := range plannedServices {
		info, err := d.DetectServiceState(ctx, serviceName, appName, app, identifier, inline, running)
		if err != nil {
			return nil, apperr.Wrap("servicestate.DetectAllServicesStateSequential", apperr.External, err, "failed to detect state for service %s/%s", appName, serviceName)
		}
		results = append(results, info)
	}
	
	return results, nil
}

// detectAllServicesStateParallel processes services concurrently for faster detection
func (d *ServiceStateDetector) detectAllServicesStateParallel(ctx context.Context, appName string, app manifest.Application, identifier string, inline []string, running map[string]dockercli.ComposePsItem, plannedServices []string) ([]ServiceInfo, error) {
	type serviceResult struct {
		info  ServiceInfo
		err   error
		order int
	}
	
	resultsChan := make(chan serviceResult, len(plannedServices))
	var wg sync.WaitGroup
	
	// Process each service concurrently
	for i, serviceName := range plannedServices {
		wg.Add(1)
		go func(serviceName string, order int) {
			defer wg.Done()
			
			info, err := d.DetectServiceState(ctx, serviceName, appName, app, identifier, inline, running)
			resultsChan <- serviceResult{info: info, err: err, order: order}
		}(serviceName, i)
	}
	
	// Wait for all services to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()
	
	// Collect results in original order to maintain deterministic output
	results := make([]serviceResult, len(plannedServices))
	for result := range resultsChan {
		results[result.order] = result
	}
	
	// Check for errors and build final results
	var finalResults []ServiceInfo
	for i, result := range results {
		if result.err != nil {
			return nil, apperr.Wrap("servicestate.DetectAllServicesStateParallel", apperr.External, result.err, "failed to detect state for service %s/%s", appName, plannedServices[i])
		}
		finalResults = append(finalResults, result.info)
	}
	
	return finalResults, nil
}

// NeedsApply determines if any services in the list require application/reconciliation.
func NeedsApply(services []ServiceInfo) bool {
	for _, service := range services {
		if service.State != ServiceRunning {
			return true
		}
	}
	return false
}

// GetServiceNames extracts service names from a list of ServiceInfo.
func GetServiceNames(services []ServiceInfo) []string {
	names := make([]string, len(services))
	for i, service := range services {
		names[i] = service.Name
	}
	return names
}
