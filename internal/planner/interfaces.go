package planner

import (
	"context"
	"io"

	"github.com/gcstr/dockform/internal/dockercli"
)

// DockerClient defines the interface for Docker operations needed by the planner.
// This allows for easy mocking in tests and potential future support for other container runtimes.
type DockerClient interface {
	// Volume operations
	ListVolumes(ctx context.Context) ([]string, error)
	CreateVolume(ctx context.Context, name string, labels map[string]string) error
	RemoveVolume(ctx context.Context, name string) error

	// Volume file operations
	ReadFileFromVolume(ctx context.Context, volumeName, targetPath, relFile string) (string, error)
	WriteFileToVolume(ctx context.Context, volumeName, targetPath, relFile, content string) error
	ExtractTarToVolume(ctx context.Context, volumeName, targetPath string, tarReader io.Reader) error
	RemovePathsFromVolume(ctx context.Context, volumeName, targetPath string, relPaths []string) error

	// Network operations
	ListNetworks(ctx context.Context) ([]string, error)
	CreateNetwork(ctx context.Context, name string, labels map[string]string, opts ...dockercli.NetworkCreateOpts) error
	RemoveNetwork(ctx context.Context, name string) error
	InspectNetwork(ctx context.Context, name string) (dockercli.NetworkInspect, error)

	// Container operations
	ListComposeContainersAll(ctx context.Context) ([]dockercli.PsBrief, error)
	ListContainersUsingVolume(ctx context.Context, volumeName string) ([]string, error)
	ListRunningContainersUsingVolume(ctx context.Context, volumeName string) ([]string, error)
	RestartContainer(ctx context.Context, name string) error
	StopContainers(ctx context.Context, names []string) error
	StartContainers(ctx context.Context, names []string) error
	RemoveContainer(ctx context.Context, name string, force bool) error
	UpdateContainerLabels(ctx context.Context, containerName string, labels map[string]string) error
	InspectContainerLabels(ctx context.Context, containerName string, keys []string) (map[string]string, error)
	InspectMultipleContainerLabels(ctx context.Context, containerNames []string, keys []string) (map[string]map[string]string, error)

	// Compose operations
	ComposeConfigFull(ctx context.Context, root string, files []string, profiles []string, envFiles []string, inline []string) (dockercli.ComposeConfigDoc, error)
	ComposeConfigServices(ctx context.Context, root string, files []string, profiles []string, envFiles []string, inline []string) ([]string, error)
	ComposeConfigHash(ctx context.Context, root string, files []string, profiles []string, envFiles []string, project, serviceName, identifier string, inline []string) (string, error)
	ComposeConfigHashes(ctx context.Context, root string, files []string, profiles []string, envFiles []string, project string, services []string, identifier string, inline []string) (map[string]string, error)
	ComposePs(ctx context.Context, root string, files []string, profiles []string, envFiles []string, project string, inline []string) ([]dockercli.ComposePsItem, error)
	ComposeUp(ctx context.Context, root string, files []string, profiles []string, envFiles []string, project string, inline []string) (string, error)
}

// Ensure that dockercli.Client implements DockerClient interface
var _ DockerClient = (*dockercli.Client)(nil)
