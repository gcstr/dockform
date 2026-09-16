package planner

import (
	"context"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
)

func TestServiceStateDetector_BuildInlineEnv(t *testing.T) {
	detector := NewServiceStateDetector(nil)

	app := manifest.Stack{
		EnvInline: []string{"FOO=bar", "BAZ=qux"},
	}

	result, err := detector.BuildInlineEnv(context.Background(), app, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"FOO=bar", "BAZ=qux"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d items, got %d", len(expected), len(result))
	}

	for i, exp := range expected {
		if result[i] != exp {
			t.Errorf("expected %q at position %d, got %q", exp, i, result[i])
		}
	}
}

func TestServiceStateDetector_DetectServiceState_Missing(t *testing.T) {
	detector := NewServiceStateDetector(nil)

	app := manifest.Stack{Root: "/tmp"}
	running := map[string]dockercli.ComposePsItem{} // Empty - no running services

	info, err := detector.DetectServiceState(context.Background(), "web", "myapp", app, "test-id", []string{}, running)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Name != "web" {
		t.Errorf("expected name 'web', got %q", info.Name)
	}
	if info.AppName != "myapp" {
		t.Errorf("expected app name 'myapp', got %q", info.AppName)
	}
	if info.State != ServiceMissing {
		t.Errorf("expected state ServiceMissing, got %v", info.State)
	}
	if info.Container != nil {
		t.Errorf("expected nil container, got %v", info.Container)
	}
}

func TestServiceStateDetector_DetectServiceState_Running(t *testing.T) {
	detector := NewServiceStateDetector(nil)

	app := manifest.Stack{Root: "/tmp"}
	container := dockercli.ComposePsItem{
		Name:    "myapp_web_1",
		Service: "web",
	}
	running := map[string]dockercli.ComposePsItem{
		"web": container,
	}

	info, err := detector.DetectServiceState(context.Background(), "web", "myapp", app, "", []string{}, running)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.State != ServiceRunning {
		t.Errorf("expected state ServiceRunning, got %v", info.State)
	}
	if info.Container == nil {
		t.Errorf("expected container to be set")
	} else if info.Container.Name != "myapp_web_1" {
		t.Errorf("expected container name 'myapp_web_1', got %q", info.Container.Name)
	}
}

func TestNeedsApply(t *testing.T) {
	tests := []struct {
		name     string
		services []ServiceInfo
		expected bool
	}{
		{
			name:     "empty list",
			services: []ServiceInfo{},
			expected: false,
		},
		{
			name: "all running",
			services: []ServiceInfo{
				{State: ServiceRunning},
				{State: ServiceRunning},
			},
			expected: false,
		},
		{
			name: "one missing",
			services: []ServiceInfo{
				{State: ServiceRunning},
				{State: ServiceMissing},
			},
			expected: true,
		},
		{
			name: "one drifted",
			services: []ServiceInfo{
				{State: ServiceRunning},
				{State: ServiceDrifted},
			},
			expected: true,
		},
		{
			name: "identifier mismatch",
			services: []ServiceInfo{
				{State: ServiceRunning},
				{State: ServiceIdentifierMismatch},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NeedsApply(tt.services)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestFormatAction_Start(t *testing.T) {
	r := NewResource(ResourceService, "netbird-server", ActionStart, "")
	if got := r.FormatAction(); got != "will be started" {
		t.Errorf("FormatAction() = %q, want %q", got, "will be started")
	}
}

func TestActionStart_IsAdditiveChangeType(t *testing.T) {
	// A start adds a running container, so it shares the create glyph rather
	// than inventing a new one.
	if got := actionToChangeType(ActionStart); got != ui.Add {
		t.Errorf("actionToChangeType(ActionStart) = %v, want ui.Add", got)
	}
}

// serviceFixture describes the mock Docker state for one service, used to
// drive detectServiceStateFast through detectStateForTest.
type serviceFixture struct {
	name            string
	stack           string
	runningService  bool
	containerExists bool
	configDrifted   bool
}

// detectStateForTest builds a mock Docker client reflecting f and runs
// detectServiceStateFast against it. runningService wires the service into
// GetRunningServices' backing ComposePs data; containerExists wires it into
// the all-containers listing (ListComposeContainersAll) used to detect a
// stopped-but-present container; configDrifted makes the container's
// recorded config-hash label disagree with the (mocked) desired hash.
func detectStateForTest(t *testing.T, f serviceFixture) ServiceInfo {
	t.Helper()

	mock := newMockDocker()
	containerName := f.stack + "_" + f.name + "_1"

	if f.runningService {
		mock.composePsItems = []dockercli.ComposePsItem{
			{Name: containerName, Service: f.name, Project: f.stack},
		}
	}

	if f.containerExists {
		mock.containers = []dockercli.PsBrief{
			{Project: f.stack, Service: f.name, Name: containerName},
		}
		hash := "mock-hash" // matches mockDockerClient.ComposeConfigHash's fixed return
		if f.configDrifted {
			hash = "stale-hash"
		}
		mock.containerLabels[containerName] = map[string]string{
			"com.docker.compose.config-hash": hash,
		}
	}

	detector := NewServiceStateDetector(mock)
	stack := manifest.Stack{Root: "/tmp/" + f.stack}

	running, err := detector.GetRunningServices(context.Background(), stack, nil)
	if err != nil {
		t.Fatalf("GetRunningServices: %v", err)
	}

	info, err := detector.DetectServiceState(context.Background(), f.name, f.stack, stack, "", nil, running)
	if err != nil {
		t.Fatalf("DetectServiceState: %v", err)
	}
	return info
}

func TestDetectServiceState_ExistingButStopped(t *testing.T) {
	// A container that exists but is not running must be ServiceStopped, not
	// ServiceMissing. ServiceMissing maps to "will be created", which is a lie
	// for a container compose will merely start.
	info := detectStateForTest(t, serviceFixture{
		name:            "netbird-server",
		stack:           "netbird",
		runningService:  false,
		containerExists: true,
		configDrifted:   false,
	})
	if info.State != ServiceStopped {
		t.Errorf("State = %v, want ServiceStopped", info.State)
	}
}

func TestDetectServiceState_StoppedAndDrifted_Recreates(t *testing.T) {
	info := detectStateForTest(t, serviceFixture{
		name:            "netbird-server",
		stack:           "netbird",
		runningService:  false,
		containerExists: true,
		configDrifted:   true,
	})
	if info.State != ServiceDrifted {
		t.Errorf("State = %v, want ServiceDrifted (recreate wins over start)", info.State)
	}
}

func TestDetectServiceState_NoContainer_StillCreates(t *testing.T) {
	// After `compose down` the container genuinely does not exist, so
	// "will be created" is correct and must not change.
	info := detectStateForTest(t, serviceFixture{
		name:            "netbird-server",
		stack:           "netbird",
		runningService:  false,
		containerExists: false,
		configDrifted:   false,
	})
	if info.State != ServiceMissing {
		t.Errorf("State = %v, want ServiceMissing", info.State)
	}
}

// TestDetectServiceState_CrossStackServiceNameCollision_NotConflated pins that
// detectStoppedContainer scopes its host-wide ListComposeContainersAll lookup
// to the stack's own resolved compose project, not just the service name.
// Two independent stacks that both happen to declare a "postgres" service,
// neither setting an explicit project.name (the common case), must not have
// stack A's stopped container mistakenly reported against stack B's
// still-nonexistent one.
func TestDetectServiceState_CrossStackServiceNameCollision_NotConflated(t *testing.T) {
	mock := newMockDocker()

	// Stack A ("postgres-a"): a stopped "postgres" container exists.
	containerA := "postgres-a_postgres_1"
	mock.containers = []dockercli.PsBrief{
		{Project: "postgres-a", Service: "postgres", Name: containerA},
	}
	mock.containerLabels[containerA] = map[string]string{
		"com.docker.compose.config-hash": "mock-hash",
	}

	detector := NewServiceStateDetector(mock)

	// Stack B ("postgres-b"): same service name, no container was ever
	// created for it. Neither stack sets project.name, so compose resolves
	// each stack's project from its own directory (mocked here as the
	// lowercased basename of Root, mirroring real compose's default).
	stackB := manifest.Stack{Root: "/stacks/postgres-b"}
	running := map[string]dockercli.ComposePsItem{} // nothing running for B

	info, err := detector.DetectServiceState(context.Background(), "postgres", "postgres-b", stackB, "", nil, running)
	if err != nil {
		t.Fatalf("DetectServiceState: %v", err)
	}
	if info.State != ServiceMissing {
		t.Errorf("State = %v, want ServiceMissing (must not conflate with stack A's unrelated stopped container)", info.State)
	}
}

func TestGetServiceNames(t *testing.T) {
	services := []ServiceInfo{
		{Name: "web"},
		{Name: "db"},
		{Name: "cache"},
	}

	names := GetServiceNames(services)

	expected := []string{"web", "db", "cache"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}

	for i, exp := range expected {
		if names[i] != exp {
			t.Errorf("expected %q at position %d, got %q", exp, i, names[i])
		}
	}
}
