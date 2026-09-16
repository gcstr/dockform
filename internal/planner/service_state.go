package planner

import (
	"context"
	"path/filepath"
	"sort"
	"sync"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/logger"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/secrets"
)

// ServiceState represents the current state of a service relative to its desired configuration.
type ServiceState int

const (
	// ServiceMissing indicates the service is not running
	ServiceMissing ServiceState = iota
	// ServiceStopped indicates a container for the service exists (compose ps -a
	// sees it) but is not currently running, and its config has not drifted.
	// Distinct from ServiceMissing so it maps to "will be started" rather than
	// the misleading "will be created".
	ServiceStopped
	// ServiceRunning indicates the service is running and up-to-date
	ServiceRunning
	// ServiceDrifted indicates the service is running but configuration has drifted
	ServiceDrifted
	// ServiceIdentifierMismatch indicates the service is running but has wrong identifier label
	ServiceIdentifierMismatch
)

// ServiceInfo contains information about a service's desired and actual state.
type ServiceInfo struct {
	Name        string
	AppName     string
	State       ServiceState
	DesiredHash string
	RunningHash string
	Container   *dockercli.ComposePsItem // nil if not running
}

// ServiceStateDetector handles detection of service state changes.
type ServiceStateDetector struct {
	docker   DockerClient
	parallel bool
}

// NewServiceStateDetector creates a new service state detector.
func NewServiceStateDetector(docker DockerClient) *ServiceStateDetector {
	return &ServiceStateDetector{docker: docker, parallel: true}
}

// WithParallel enables or disables parallel processing for service state detection.
func (d *ServiceStateDetector) WithParallel(enabled bool) *ServiceStateDetector {
	d.parallel = enabled
	return d
}

// GetPlannedServices returns the list of services defined in the stack's compose files.
func (d *ServiceStateDetector) GetPlannedServices(ctx context.Context, stack manifest.Stack, inline []string) ([]string, error) {
	if d.docker == nil {
		return nil, nil
	}

	// Prefer cheap service listing first
	services, err := d.docker.ComposeConfigServices(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, inline)
	if err == nil && len(services) > 0 {
		sort.Strings(services)
		return services, nil
	}

	// Fallback to full config parse if needed
	doc, err2 := d.docker.ComposeConfigFull(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, inline)
	if err2 != nil {
		return nil, err2
	}
	out := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// BuildInlineEnv constructs the inline environment variables for a stack, including SOPS secrets.
func (d *ServiceStateDetector) BuildInlineEnv(ctx context.Context, stack manifest.Stack, sopsConfig *manifest.SopsConfig) ([]string, error) {
	inline := append([]string(nil), stack.EnvInline...)

	ageKeyFile := ""
	pgpDir := ""
	pgpAgent := false
	pgpMode := ""
	pgpPass := ""
	if sopsConfig != nil && sopsConfig.Age != nil {
		ageKeyFile = sopsConfig.Age.KeyFile
	}
	if sopsConfig != nil && sopsConfig.Pgp != nil {
		pgpDir = sopsConfig.Pgp.KeyringDir
		pgpAgent = sopsConfig.Pgp.UseAgent
		pgpMode = sopsConfig.Pgp.PinentryMode
		pgpPass = sopsConfig.Pgp.Passphrase
	}

	for _, pth0 := range stack.SopsSecrets {
		pth := pth0
		if pth != "" && !filepath.IsAbs(pth) {
			pth = filepath.Join(stack.Root, pth)
		}
		pairs, err := secrets.DecryptAndParse(ctx, pth, secrets.SopsOptions{
			AgeKeyFile:      ageKeyFile,
			PgpKeyringDir:   pgpDir,
			PgpUseAgent:     pgpAgent,
			PgpPinentryMode: pgpMode,
			PgpPassphrase:   pgpPass,
		})
		if err != nil {
			return nil, apperr.Wrap("servicestate.BuildInlineEnv", apperr.External, err, "decrypt sops secret %s", pth)
		}
		inline = append(inline, pairs...)
	}

	return inline, nil
}

// GetRunningServices returns a map of currently running services for the stack.
func (d *ServiceStateDetector) GetRunningServices(ctx context.Context, stack manifest.Stack, inline []string) (map[string]dockercli.ComposePsItem, error) {
	running := map[string]dockercli.ComposePsItem{}

	if d.docker == nil {
		return running, nil
	}

	proj := ""
	if stack.Project != nil {
		proj = stack.Project.Name
	}

	items, err := d.docker.ComposePs(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, proj, inline)
	if err != nil {
		// Never treat a failed ps as "nothing running": that plans creates for a live stack.
		return nil, apperr.Wrap("servicestate.GetRunningServices", apperr.External, err, "list running containers for stack %s", stack.Root)
	}

	for _, item := range items {
		running[item.Service] = item
	}

	return running, nil
}

// DetectServiceState determines the state of a single service.
func (d *ServiceStateDetector) DetectServiceState(ctx context.Context, serviceName, stackName string, stack manifest.Stack, identifier string, inline []string, running map[string]dockercli.ComposePsItem) (ServiceInfo, error) {
	return d.detectServiceStateFast(ctx, serviceName, stackName, stack, identifier, inline, running, nil, nil)
}

// detectServiceStateFast determines the state of a single service using precomputed data where available.
// If desiredHashes or labelsByContainer are nil or missing entries, it falls back to computing them.
func (d *ServiceStateDetector) detectServiceStateFast(ctx context.Context, serviceName, stackName string, stack manifest.Stack, identifier string, inline []string, running map[string]dockercli.ComposePsItem, desiredHashes map[string]string, labelsByContainer map[string]map[string]string) (ServiceInfo, error) {
	info := ServiceInfo{
		Name:    serviceName,
		AppName: stackName,
		State:   ServiceMissing,
	}

	// Check running first
	container, isRunning := running[serviceName]
	if isRunning {
		info.Container = &container
		info.State = ServiceRunning
	}

	if d.docker == nil {
		return info, nil
	}

	// Project name
	proj := ""
	if stack.Project != nil {
		proj = stack.Project.Name
	}

	// Desired hash from precomputed map or compute on demand
	var desiredHash string
	if desiredHashes != nil {
		desiredHash = desiredHashes[serviceName]
	}
	if desiredHash == "" {
		// Without a desired hash the drift check is skipped, hiding real drift.
		dh, err := d.docker.ComposeConfigHash(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, proj, serviceName, identifier, inline)
		if err != nil {
			return info, apperr.Wrap("servicestate.DetectServiceState", apperr.External, err, "compute desired config hash for service %s/%s", stackName, serviceName)
		}
		desiredHash = dh
	}
	info.DesiredHash = desiredHash

	if !isRunning {
		d.detectStoppedContainer(ctx, &info, serviceName, proj, desiredHash)
		return info, nil
	}

	// Labels: prefer batch result
	var labels map[string]string
	var err error
	keys := []string{"com.docker.compose.config-hash"}
	if identifier != "" {
		keys = append(keys, "io.dockform.identifier")
	}
	if labelsByContainer != nil && info.Container != nil {
		labels = labelsByContainer[info.Container.Name]
	}
	if labels == nil {
		labels, err = d.docker.InspectContainerLabels(ctx, info.Container.Name, keys)
		if err != nil {
			// Reporting "drifted" here would plan a recreate on a transient failure.
			return info, apperr.Wrap("servicestate.DetectServiceState", apperr.External, err, "inspect labels of container %s", info.Container.Name)
		}
	}

	// Identifier check
	if identifier != "" {
		if v, ok := labels["io.dockform.identifier"]; !ok || v != identifier {
			info.State = ServiceIdentifierMismatch
			return info, nil
		}
	}

	// Hash drift check if desired hash available
	if desiredHash != "" {
		runningHash := labels["com.docker.compose.config-hash"]
		info.RunningHash = runningHash
		if runningHash == "" || runningHash != desiredHash {
			info.State = ServiceDrifted
			return info, nil
		}
	}

	return info, nil
}

// detectStoppedContainer upgrades info from ServiceMissing to ServiceStopped
// (or ServiceDrifted) when a container for this service already exists but
// is not running — e.g. `compose stop`, or a crashed/exited container. This
// must NOT fire after `compose down`, which removes the container entirely:
// in that case ListComposeContainersAll finds nothing and info stays
// ServiceMissing, which correctly reports "will be created".
func (d *ServiceStateDetector) detectStoppedContainer(ctx context.Context, info *ServiceInfo, serviceName, proj, desiredHash string) {
	all, err := d.docker.ListComposeContainersAll(ctx)
	if err != nil {
		// Can't tell whether a container exists; leave the ServiceMissing default.
		return
	}

	for _, c := range all {
		if c.Service != serviceName {
			continue
		}
		if proj != "" && normalizeComposeProject(c.Project) != normalizeComposeProject(proj) {
			continue
		}

		info.State = ServiceStopped
		if desiredHash != "" {
			labels, lerr := d.docker.InspectContainerLabels(ctx, c.Name, []string{"com.docker.compose.config-hash"})
			if lerr == nil {
				runningHash := labels["com.docker.compose.config-hash"]
				info.RunningHash = runningHash
				if runningHash == "" || runningHash != desiredHash {
					info.State = ServiceDrifted
				}
			}
		}
		return
	}
}

// DetectAllServicesState analyzes the state of all services in a stack.
func (d *ServiceStateDetector) DetectAllServicesState(ctx context.Context, stackName string, stack manifest.Stack, identifier string, sopsConfig *manifest.SopsConfig) ([]ServiceInfo, error) {
	// Build inline environment
	inline, err := d.BuildInlineEnv(ctx, stack, sopsConfig)
	if err != nil {
		return nil, err
	}

	// Get planned services
	plannedServices, err := d.GetPlannedServices(ctx, stack, inline)
	if err != nil {
		return nil, apperr.Wrap("servicestate.DetectAllServicesState", apperr.External, err, "failed to get planned services for stack %s", stackName)
	}

	if len(plannedServices) == 0 {
		return nil, nil
	}

	// Get running services
	running, err := d.GetRunningServices(ctx, stack, inline)
	if err != nil {
		return nil, apperr.Wrap("servicestate.DetectAllServicesState", apperr.External, err, "failed to get running services for stack %s", stackName)
	}

	// Precompute desired hashes for all planned services (reuse overlay once)
	desiredHashes := map[string]string{}
	if d.docker != nil && len(plannedServices) > 0 {
		proj := ""
		if stack.Project != nil {
			proj = stack.Project.Name
		}
		hashes, err := d.docker.ComposeConfigHashes(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, proj, plannedServices, identifier, inline)
		if err != nil {
			// Correct when degraded: each service's hash is then computed on its own.
			logger.FromContext(ctx).Debug("config_hashes_batch_failed", "stack", stackName, "error", err)
		} else {
			desiredHashes = hashes
		}
	}

	// Batch container label inspection for running containers
	labelsByContainer := map[string]map[string]string{}
	if d.docker != nil && len(running) > 0 {
		names := make([]string, 0, len(running))
		for _, it := range running {
			names = append(names, it.Name)
		}
		keys := []string{"com.docker.compose.config-hash"}
		if identifier != "" {
			keys = append(keys, "io.dockform.identifier")
		}
		got, err := d.docker.InspectMultipleContainerLabels(ctx, names, keys)
		if err != nil {
			// Correct when degraded: each container's labels are then inspected on their own.
			logger.FromContext(ctx).Debug("container_labels_batch_failed", "stack", stackName, "error", err)
		} else if got != nil {
			labelsByContainer = got
		}
	}

	// Choose parallel or sequential processing based on configuration
	if d.parallel {
		return d.detectAllServicesStateParallel(ctx, stackName, stack, identifier, inline, running, plannedServices, desiredHashes, labelsByContainer)
	}
	return d.detectAllServicesStateSequential(ctx, stackName, stack, identifier, inline, running, plannedServices, desiredHashes, labelsByContainer)
}

// detectAllServicesStateSequential processes services one by one (original implementation)
func (d *ServiceStateDetector) detectAllServicesStateSequential(ctx context.Context, stackName string, stack manifest.Stack, identifier string, inline []string, running map[string]dockercli.ComposePsItem, plannedServices []string, desiredHashes map[string]string, labelsByContainer map[string]map[string]string) ([]ServiceInfo, error) {
	// Analyze each planned service
	var results []ServiceInfo
	for _, serviceName := range plannedServices {
		info, err := d.detectServiceStateFast(ctx, serviceName, stackName, stack, identifier, inline, running, desiredHashes, labelsByContainer)
		if err != nil {
			return nil, apperr.Wrap("servicestate.DetectAllServicesStateSequential", apperr.External, err, "failed to detect state for service %s/%s", stackName, serviceName)
		}
		results = append(results, info)
	}

	return results, nil
}

// detectAllServicesStateParallel processes services concurrently for faster detection
func (d *ServiceStateDetector) detectAllServicesStateParallel(ctx context.Context, stackName string, stack manifest.Stack, identifier string, inline []string, running map[string]dockercli.ComposePsItem, plannedServices []string, desiredHashes map[string]string, labelsByContainer map[string]map[string]string) ([]ServiceInfo, error) {
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

			info, err := d.detectServiceStateFast(ctx, serviceName, stackName, stack, identifier, inline, running, desiredHashes, labelsByContainer)
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
			return nil, apperr.Wrap("servicestate.DetectAllServicesStateParallel", apperr.External, result.err, "failed to detect state for service %s/%s", stackName, plannedServices[i])
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
