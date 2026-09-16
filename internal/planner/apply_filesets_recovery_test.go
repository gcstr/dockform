package planner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

func coldFilesetConfig(t *testing.T, sourceDir string) manifest.Config {
	t.Helper()
	return manifest.Config{
		Identifier: "demo",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{
			"assets": {
				Context:      "default",
				SourceAbs:    sourceDir,
				TargetVolume: "data",
				TargetPath:   "/opt/data",
				ApplyMode:    "cold",
				RestartServices: manifest.RestartTargets{
					Services: []string{"web"},
				},
			},
		},
	}
}

func TestSyncFilesetsForContext_ColdFailureRestartSuccessReturnsBaseError(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	mockDocker := newMockDocker()
	mockDocker.containers = []dockercli.PsBrief{{Project: "demo", Service: "web", Name: "demo-web-1"}}
	mockDocker.extractTarError = errors.New("extract failed")

	fm := NewFilesetManager(mockDocker, nil)
	_, _, err := fm.SyncFilesetsForContext(
		context.Background(),
		coldFilesetConfig(t, src),
		"default",
		map[string]struct{}{"data": {}},
		nil,
	)
	if err == nil {
		t.Fatalf("expected sync failure")
	}
	if strings.Contains(err.Error(), "restart also failed") {
		t.Fatalf("should not claim restart failure when restart succeeded: %v", err)
	}
	if !strings.Contains(err.Error(), "extract tar for fileset assets") {
		t.Fatalf("expected base sync error, got: %v", err)
	}
	if len(mockDocker.stoppedContainers) != 1 || mockDocker.stoppedContainers[0] != "demo-web-1" {
		t.Fatalf("expected cold container stop call, got: %#v", mockDocker.stoppedContainers)
	}
	if len(mockDocker.startedContainers) != 1 || mockDocker.startedContainers[0] != "demo-web-1" {
		t.Fatalf("expected cold container restart call, got: %#v", mockDocker.startedContainers)
	}
}

func TestSyncFilesetsForContext_ColdFailureRestartFailureReturnsAggregate(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	mockDocker := newMockDocker()
	mockDocker.containers = []dockercli.PsBrief{{Project: "demo", Service: "web", Name: "demo-web-1"}}
	mockDocker.extractTarError = errors.New("extract failed")
	mockDocker.startContainersError = errors.New("start failed")

	fm := NewFilesetManager(mockDocker, nil)
	_, _, err := fm.SyncFilesetsForContext(
		context.Background(),
		coldFilesetConfig(t, src),
		"default",
		map[string]struct{}{"data": {}},
		nil,
	)
	if err == nil {
		t.Fatalf("expected sync failure")
	}
	if !strings.Contains(err.Error(), "fileset sync failed and cold-mode service restart also failed") {
		t.Fatalf("expected aggregate restart failure message, got: %v", err)
	}
	if !apperr.IsKind(err, apperr.External) {
		t.Fatalf("expected external error kind, got: %v", err)
	}
}

// A nil docker client must fail with a clear precondition error, not panic.
// The projectToStack computation used to run before the nil-docker guard, and
// for a stack with no project.name it reaches straight into
// client.ComposeConfigFull on a nil DockerClient interface.
func TestSyncFilesetsForContext_NilDockerReturnsPreconditionNotPanic(t *testing.T) {
	cfg := manifest.Config{
		Identifier: "demo",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
		Stacks: map[string]manifest.Stack{
			// No Project.Name: forces stackProjectMap to call ComposeConfigFull.
			"default/web": {Root: "/stacks/web"},
		},
		DiscoveredFilesets: coldFilesetConfig(t, t.TempDir()).DiscoveredFilesets,
	}

	fm := NewFilesetManager(nil, nil)
	_, _, err := fm.SyncFilesetsForContext(context.Background(), cfg, "default", map[string]struct{}{}, nil)
	if err == nil {
		t.Fatalf("expected precondition error for nil docker client")
	}
	if !apperr.IsKind(err, apperr.Precondition) {
		t.Fatalf("expected precondition error kind, got: %v", err)
	}
}
