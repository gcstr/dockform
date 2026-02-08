package planner

import (
	"context"
	"io"
	"strings"

	"github.com/gcstr/dockform/internal/dockercli"
)

// mockDockerClient provides a mock implementation of DockerClient for testing.
type mockDockerClient struct {
	// Mock data to return
	volumes         []string
	networks        []string
	containers      []dockercli.PsBrief
	composePsItems  []dockercli.ComposePsItem
	volumeFiles     map[string]string            // volumeName -> file content
	containerLabels map[string]map[string]string // containerName -> labels

	// Track operations performed
	createdVolumes      []string
	createdNetworks     []string
	restartedContainers []string
	startedContainers   []string
	stoppedContainers   []string
	removedContainers   []string
	removedVolumes      []string
	removedNetworks     []string
	writtenFiles        map[string]string   // fileName -> content
	extractedTars       []string            // volume names that had tars extracted
	removedPaths        map[string][]string // volumeName -> removed paths
	runVolumeScriptRuns int

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
func (m *mockDockerClient) ListVolumes(ctx context.Context) ([]string, error) {
	if m.listVolumesError != nil {
		return nil, m.listVolumesError
	}
	return m.volumes, nil
}

func (m *mockDockerClient) CreateVolume(ctx context.Context, name string, labels map[string]string) error {
	if m.createVolumeError != nil {
		return m.createVolumeError
	}
	m.createdVolumes = append(m.createdVolumes, name)
	m.volumes = append(m.volumes, name)
	return nil
}

func (m *mockDockerClient) RemoveVolume(ctx context.Context, name string) error {
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
	content, exists := m.volumeFiles[volumeName]
	if !exists {
		return "", nil
	}
	return content, nil
}

func (m *mockDockerClient) RunVolumeScript(ctx context.Context, volumeName, targetPath, script string, env []string) (dockercli.VolumeScriptResult, error) {
	m.runVolumeScriptRuns++
	// Mock implementation - just return success
	if m.runVolumeScriptError != nil {
		return dockercli.VolumeScriptResult{}, m.runVolumeScriptError
	}
	return dockercli.VolumeScriptResult{Stdout: "Ownership applied successfully\n"}, nil
}

func (m *mockDockerClient) WriteFileToVolume(ctx context.Context, volumeName, targetPath, relFile, content string) error {
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
	if m.extractTarError != nil {
		return m.extractTarError
	}
	m.extractedTars = append(m.extractedTars, volumeName)
	return nil
}

func (m *mockDockerClient) RemovePathsFromVolume(ctx context.Context, volumeName, targetPath string, relPaths []string) error {
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
	if m.listNetworksError != nil {
		return nil, m.listNetworksError
	}
	return m.networks, nil
}

func (m *mockDockerClient) CreateNetwork(ctx context.Context, name string, labels map[string]string, opts ...dockercli.NetworkCreateOpts) error {
	if m.createNetworkError != nil {
		return m.createNetworkError
	}
	m.createdNetworks = append(m.createdNetworks, name)
	m.networks = append(m.networks, name)
	return nil
}

func (m *mockDockerClient) RemoveNetwork(ctx context.Context, name string) error {
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
	if m.listComposeContainersError != nil {
		return nil, m.listComposeContainersError
	}
	return m.containers, nil
}

func (m *mockDockerClient) ListContainersUsingVolume(ctx context.Context, volumeName string) ([]string, error) {
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
	if m.restartError != nil {
		return m.restartError
	}
	m.restartedContainers = append(m.restartedContainers, name)
	return nil
}

func (m *mockDockerClient) StopContainers(ctx context.Context, names []string) error {
	if m.stopContainersError != nil {
		return m.stopContainersError
	}
	m.stoppedContainers = append(m.stoppedContainers, names...)
	return nil
}

func (m *mockDockerClient) StartContainers(ctx context.Context, names []string) error {
	if m.startContainersError != nil {
		return m.startContainersError
	}
	m.startedContainers = append(m.startedContainers, names...)
	return nil
}

func (m *mockDockerClient) RemoveContainer(ctx context.Context, name string, force bool) error {
	m.removedContainers = append(m.removedContainers, name)
	return nil
}

func (m *mockDockerClient) UpdateContainerLabels(ctx context.Context, containerName string, labels map[string]string) error {
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
	// Return a valid config with nginx service for website directory
	if strings.Contains(root, "website") {
		return dockercli.ComposeConfigDoc{
			Services: map[string]dockercli.ComposeService{
				"nginx": {Image: "nginx:latest"},
			},
		}, nil
	}
	return dockercli.ComposeConfigDoc{}, nil
}

func (m *mockDockerClient) ComposeConfigServices(ctx context.Context, root string, files []string, profiles []string, envFiles []string, inline []string) ([]string, error) {
	return []string{}, nil
}

func (m *mockDockerClient) ComposeConfigHash(ctx context.Context, root string, files []string, profiles []string, envFiles []string, project, serviceName, identifier string, inline []string) (string, error) {
	return "mock-hash", nil
}

func (m *mockDockerClient) ComposePs(ctx context.Context, root string, files []string, profiles []string, envFiles []string, project string, inline []string) ([]dockercli.ComposePsItem, error) {
	return m.composePsItems, nil
}

func (m *mockDockerClient) ComposeUp(ctx context.Context, root string, files []string, profiles []string, envFiles []string, project string, inline []string) (string, error) {
	return "compose up output", nil
}

// Batch container operations
func (m *mockDockerClient) InspectContainerLabelsBatch(ctx context.Context, containers []string, labelKeys []string) (map[string]map[string]string, error) {
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
