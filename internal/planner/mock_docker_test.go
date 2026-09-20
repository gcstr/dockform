package planner

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gcstr/dockform/internal/dockercli"
)

// mockDockerClient provides a mock implementation of DockerClient for testing.
// BuildPlan fans out over stacks in parallel goroutines, so a single mock
// instance is hit concurrently by tests. mu guards every field below.
type mockDockerClient struct {
	mu sync.Mutex

	// Mock data to return
	volumes         []string
	allVolumes      []string
	networks        []string
	composeNetworks map[string]string // subset of networks owned by a compose stack
	// root -> project name compose resolves (default: lowercased directory name)
	composeProjectNames    map[string]string
	composeConfigFullError error
	composePsError         error
	composeConfigHashError error
	inspectLabelsError     error
	containers             []dockercli.PsBrief
	composePsItems         []dockercli.ComposePsItem
	volumeFiles            map[string]string            // volumeName -> file content
	containerLabels        map[string]map[string]string // containerName -> labels
	// composeConfigDocs, keyed by stack root, overrides ComposeConfigFull's
	// default document when set.
	composeConfigDocs map[string]dockercli.ComposeConfigDoc
	// progressEvents are replayed into ComposeUpWithProgress's onEvent, the way
	// real compose streams them.
	progressEvents []dockercli.ComposeEvent

	// Track operations performed
	createdVolumes         []string
	createdNetworks        []string
	restartedContainers    []string
	startedContainers      []string
	stoppedContainers      []string
	removedContainers      []string
	removedVolumes         []string
	removedNetworks        []string
	writtenFiles           map[string]string   // fileName -> content
	extractedTars          []string            // volume names that had tars extracted
	removedPaths           map[string][]string // volumeName -> removed paths
	runVolumeScriptRuns    int
	readIndexBatchCalls    int
	composeConfigFullCalls int

	// Control behavior
	listVolumesError             error
	listNetworksError            error
	createVolumeError            error
	createNetworkError           error
	listComposeContainersError   error
	listContainersUsingVolError  error
	stopContainersError          error
	startContainersError         error
	restartError                 error
	writeFileError               error
	extractTarError              error
	removePathsError             error
	runVolumeScriptError         error
	containersUsingVolume        []string
	runningContainersUsingVolume []string
}

// newMockDocker creates a new mock Docker client with sensible defaults.
func newMockDocker() *mockDockerClient {
	return &mockDockerClient{
		volumes:             []string{},
		networks:            []string{},
		containers:          []dockercli.PsBrief{},
		composePsItems:      []dockercli.ComposePsItem{},
		volumeFiles:         map[string]string{},
		containerLabels:     map[string]map[string]string{},
		createdVolumes:      []string{},
		createdNetworks:     []string{},
		restartedContainers: []string{},
		startedContainers:   []string{},
		stoppedContainers:   []string{},
		removedContainers:   []string{},
		removedVolumes:      []string{},
		removedNetworks:     []string{},
		writtenFiles:        map[string]string{},
		extractedTars:       []string{},
		removedPaths:        map[string][]string{},
	}
}

// Volume operations
func (m *mockDockerClient) ListAllVolumes(ctx context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listVolumesError != nil {
		return nil, m.listVolumesError
	}
	if m.allVolumes != nil {
		return append([]string(nil), m.allVolumes...), nil
	}
	return append([]string(nil), m.volumes...), nil
}

func (m *mockDockerClient) ListVolumes(ctx context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listVolumesError != nil {
		return nil, m.listVolumesError
	}
	return append([]string(nil), m.volumes...), nil
}

func (m *mockDockerClient) CreateVolume(ctx context.Context, name string, labels map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createVolumeError != nil {
		return m.createVolumeError
	}
	m.createdVolumes = append(m.createdVolumes, name)
	m.volumes = append(m.volumes, name)
	return nil
}

func (m *mockDockerClient) RemoveVolume(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removedVolumes = append(m.removedVolumes, name)
	// Remove from volumes slice
	for i, v := range m.volumes {
		if v == name {
			m.volumes = append(m.volumes[:i], m.volumes[i+1:]...)
			break
		}
	}
	return nil
}

// Volume file operations
func (m *mockDockerClient) ReadFileFromVolume(ctx context.Context, volumeName, targetPath, relFile string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	content, exists := m.volumeFiles[volumeName]
	if !exists {
		return "", nil
	}
	return content, nil
}

func (m *mockDockerClient) ReadIndexFilesFromVolumes(ctx context.Context, volumeNames []string, relFile string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readIndexBatchCalls++
	res := make(map[string]string, len(volumeNames))
	for _, v := range volumeNames {
		res[v] = m.volumeFiles[v] // "" when absent, mirroring ReadFileFromVolume
	}
	return res, nil
}

func (m *mockDockerClient) RunVolumeScript(ctx context.Context, volumeName, targetPath, script string, env []string) (dockercli.VolumeScriptResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runVolumeScriptRuns++
	// Mock implementation - just return success
	if m.runVolumeScriptError != nil {
		return dockercli.VolumeScriptResult{}, m.runVolumeScriptError
	}
	return dockercli.VolumeScriptResult{Stdout: "Ownership applied successfully\n"}, nil
}

func (m *mockDockerClient) WriteFileToVolume(ctx context.Context, volumeName, targetPath, relFile, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeFileError != nil {
		return m.writeFileError
	}
	if m.writtenFiles == nil {
		m.writtenFiles = make(map[string]string)
	}
	m.writtenFiles[relFile] = content
	return nil
}

func (m *mockDockerClient) ExtractTarToVolume(ctx context.Context, volumeName, targetPath string, tarReader io.Reader) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.extractTarError != nil {
		return m.extractTarError
	}
	m.extractedTars = append(m.extractedTars, volumeName)
	return nil
}

func (m *mockDockerClient) RemovePathsFromVolume(ctx context.Context, volumeName, targetPath string, relPaths []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.removePathsError != nil {
		return m.removePathsError
	}
	if m.removedPaths == nil {
		m.removedPaths = make(map[string][]string)
	}
	m.removedPaths[volumeName] = append(m.removedPaths[volumeName], relPaths...)
	return nil
}

// Network operations
func (m *mockDockerClient) ListNetworks(ctx context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listNetworksError != nil {
		return nil, m.listNetworksError
	}
	return append([]string(nil), m.networks...), nil
}

func (m *mockDockerClient) ListComposeNetworks(ctx context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listNetworksError != nil {
		return nil, m.listNetworksError
	}
	out := make(map[string]string, len(m.composeNetworks))
	for k, v := range m.composeNetworks {
		out[k] = v
	}
	return out, nil
}

func (m *mockDockerClient) CreateNetwork(ctx context.Context, name string, labels map[string]string, opts ...dockercli.NetworkCreateOpts) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createNetworkError != nil {
		return m.createNetworkError
	}
	m.createdNetworks = append(m.createdNetworks, name)
	m.networks = append(m.networks, name)
	return nil
}

func (m *mockDockerClient) RemoveNetwork(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removedNetworks = append(m.removedNetworks, name)
	// Remove from networks slice
	for i, n := range m.networks {
		if n == name {
			m.networks = append(m.networks[:i], m.networks[i+1:]...)
			break
		}
	}
	return nil
}

func (m *mockDockerClient) InspectNetwork(ctx context.Context, name string) (dockercli.NetworkInspect, error) {
	return dockercli.NetworkInspect{Name: name}, nil
}

// Container operations
func (m *mockDockerClient) ListComposeContainersAll(ctx context.Context) ([]dockercli.PsBrief, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listComposeContainersError != nil {
		return nil, m.listComposeContainersError
	}
	return append([]dockercli.PsBrief(nil), m.containers...), nil
}

func (m *mockDockerClient) ListContainersUsingVolume(ctx context.Context, volumeName string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listContainersUsingVolError != nil {
		return nil, m.listContainersUsingVolError
	}
	if m.containersUsingVolume != nil {
		return append([]string(nil), m.containersUsingVolume...), nil
	}
	// For tests, return all container names to simulate volume attachment
	var out []string
	for _, c := range m.containers {
		out = append(out, c.Name)
	}
	return out, nil
}

func (m *mockDockerClient) ListRunningContainersUsingVolume(ctx context.Context, volumeName string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runningContainersUsingVolume != nil {
		return append([]string(nil), m.runningContainersUsingVolume...), nil
	}
	// For tests that need it, derive from containers slice by matching a label or name
	// Here we just return any container names we have to simulate running ones
	out := []string{}
	for _, c := range m.containers {
		out = append(out, c.Name)
	}
	return out, nil
}

func (m *mockDockerClient) RestartContainer(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.restartError != nil {
		return m.restartError
	}
	m.restartedContainers = append(m.restartedContainers, name)
	return nil
}

func (m *mockDockerClient) StopContainers(ctx context.Context, names []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopContainersError != nil {
		return m.stopContainersError
	}
	m.stoppedContainers = append(m.stoppedContainers, names...)
	return nil
}

func (m *mockDockerClient) StartContainers(ctx context.Context, names []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startContainersError != nil {
		return m.startContainersError
	}
	m.startedContainers = append(m.startedContainers, names...)
	return nil
}

func (m *mockDockerClient) RemoveContainer(ctx context.Context, name string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removedContainers = append(m.removedContainers, name)
	return nil
}

func (m *mockDockerClient) UpdateContainerLabels(ctx context.Context, containerName string, labels map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.containerLabels == nil {
		m.containerLabels = make(map[string]map[string]string)
	}
	if m.containerLabels[containerName] == nil {
		m.containerLabels[containerName] = make(map[string]string)
	}
	for k, v := range labels {
		m.containerLabels[containerName][k] = v
	}
	return nil
}

func (m *mockDockerClient) InspectContainerLabels(ctx context.Context, containerName string, keys []string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inspectLabelsError != nil {
		return nil, m.inspectLabelsError
	}
	result := make(map[string]string)
	if containerLabels, exists := m.containerLabels[containerName]; exists {
		for _, key := range keys {
			if value, hasKey := containerLabels[key]; hasKey {
				result[key] = value
			}
		}
	}
	return result, nil
}

// Compose operations (minimal implementations for testing)
func (m *mockDockerClient) ComposeConfigFull(ctx context.Context, root string, files []string, profiles []string, envFiles []string, inline []string) (dockercli.ComposeConfigDoc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.composeConfigFullCalls++
	if m.composeConfigFullError != nil {
		return dockercli.ComposeConfigDoc{}, m.composeConfigFullError
	}
	if doc, ok := m.composeConfigDocs[root]; ok {
		return doc, nil
	}
	// Mirror compose's default project name (the directory) unless a test overrides it.
	name := strings.ToLower(filepath.Base(root))
	if override, ok := m.composeProjectNames[root]; ok {
		name = override
	}
	// Real compose honors COMPOSE_PROJECT_NAME from the environment ahead of
	// the directory-derived default. Mirror that here so a test can prove
	// inline env changes project resolution (stackProjectMap's whole reason
	// for taking one) — previously this method ignored `inline` entirely.
	for _, kv := range inline {
		if v, ok := strings.CutPrefix(kv, "COMPOSE_PROJECT_NAME="); ok && v != "" {
			name = v
		}
	}
	// Return a valid config with nginx service for website directory
	if strings.Contains(root, "website") {
		return dockercli.ComposeConfigDoc{
			Name: name,
			Services: map[string]dockercli.ComposeService{
				"nginx": {Image: "nginx:latest"},
			},
		}, nil
	}
	return dockercli.ComposeConfigDoc{Name: name}, nil
}

func (m *mockDockerClient) ComposeConfigServices(ctx context.Context, root string, files []string, profiles []string, envFiles []string, inline []string) ([]string, error) {
	return []string{}, nil
}

func (m *mockDockerClient) ComposeConfigHash(ctx context.Context, root string, files []string, profiles []string, envFiles []string, project, serviceName, identifier string, inline []string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.composeConfigHashError != nil {
		return "", m.composeConfigHashError
	}
	return "mock-hash", nil
}

func (m *mockDockerClient) ComposePs(ctx context.Context, root string, files []string, profiles []string, envFiles []string, project string, inline []string) ([]dockercli.ComposePsItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.composePsError != nil {
		return nil, m.composePsError
	}
	return append([]dockercli.ComposePsItem(nil), m.composePsItems...), nil
}

func (m *mockDockerClient) ComposeUp(ctx context.Context, root string, files []string, profiles []string, envFiles []string, project string, inline []string) (string, error) {
	return "compose up output", nil
}

func (m *mockDockerClient) ComposeUpWithProgress(ctx context.Context, root string, files []string, profiles []string, envFiles []string, project string, inline []string, onEvent func(dockercli.ComposeEvent)) (string, error) {
	m.mu.Lock()
	events := append([]dockercli.ComposeEvent(nil), m.progressEvents...)
	m.mu.Unlock()
	for _, ev := range events {
		onEvent(ev)
	}
	return "compose up output", nil
}

// Batch container operations
func (m *mockDockerClient) InspectContainerLabelsBatch(ctx context.Context, containers []string, labelKeys []string) (map[string]map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]map[string]string)
	for _, container := range containers {
		if containerLabels, exists := m.containerLabels[container]; exists {
			containerResult := make(map[string]string)
			for _, key := range labelKeys {
				if value, hasKey := containerLabels[key]; hasKey {
					containerResult[key] = value
				}
			}
			result[container] = containerResult
		}
	}
	return result, nil
}

func (m *mockDockerClient) InspectMultipleContainerLabels(ctx context.Context, containerNames []string, keys []string) (map[string]map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]map[string]string)
	for _, name := range containerNames {
		if labels, ok := m.containerLabels[name]; ok {
			filtered := make(map[string]string)
			for _, k := range keys {
				if v, has := labels[k]; has {
					filtered[k] = v
				}
			}
			result[name] = filtered
		}
	}
	return result, nil
}

// Directory sync operations
func (m *mockDockerClient) SyncDirToVolume(ctx context.Context, volumeName, targetPath, localDir string) error {
	return nil
}

// Daemon check
func (m *mockDockerClient) CheckDaemon(ctx context.Context) error {
	return nil
}

func (m *mockDockerClient) ComposeConfigHashes(ctx context.Context, root string, files []string, profiles []string, envFiles []string, project string, services []string, identifier string, inline []string) (map[string]string, error) {
	out := make(map[string]string)
	for _, s := range services {
		out[s] = "mock-hash"
	}
	return out, nil
}
