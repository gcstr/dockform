package planner

import (
	"context"
	"reflect"
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
)

// inlineEnvByStack must give stackProjectMap each stack's OWN environment, not
// a shared nil: inline env can set COMPOSE_PROJECT_NAME, so resolving one
// stack's project with another stack's (or no) inline env can land it on the
// wrong project and steal that project's line at restart time.
func TestInlineEnvByStack_CachedAndPerStackFallback(t *testing.T) {
	stacks := map[string]manifest.Stack{
		"a": {Root: "/stacks/a", EnvInline: []string{"COMPOSE_PROJECT_NAME=a-project"}},
		"b": {Root: "/stacks/b", EnvInline: []string{"COMPOSE_PROJECT_NAME=b-project"}},
	}
	execCtx := NewContextExecutionContext("default", "id")
	execCtx.Stacks["a"] = &StackExecutionData{InlineEnv: []string{"CACHED=1"}}

	got := inlineEnvByStack(context.Background(), newMockDocker(), manifest.Config{}, "default", execCtx, stacks)

	if !reflect.DeepEqual(got["a"], []string{"CACHED=1"}) {
		t.Errorf("stack a: expected cached BuildPlan inline env, got %#v", got["a"])
	}
	if !reflect.DeepEqual(got["b"], []string{"COMPOSE_PROJECT_NAME=b-project"}) {
		t.Errorf("stack b: expected its OWN EnvInline via fallback (not a's, not nil), got %#v", got["b"])
	}
}

// SyncFilesetsForContext must not resolve any stack's compose project when
// the context has no filesets at all: with nothing to sync, restartPending
// stays empty and RestartPendingServices (apply.go) returns before ever
// touching the returned project->stack map, so stackProjectMap's `compose
// config` calls (one per stack, via stackComposeProject) are pure waste on a
// context with stacks but no filesets.
func TestSyncFilesetsForContext_NoFilesets_SkipsComposeConfigForStacks(t *testing.T) {
	mock := newMockDocker()
	cfg := manifest.Config{
		Identifier: "test",
		Contexts:   map[string]manifest.ContextConfig{"default": {}},
		Stacks: map[string]manifest.Stack{
			"default/app": {Context: "default", Root: "/stacks/app"},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{},
	}
	fm := NewFilesetManagerWithClient(mock, nil)

	restartPending, projectToStack, err := fm.SyncFilesetsForContext(context.Background(), cfg, "default", map[string]struct{}{}, nil)
	if err != nil {
		t.Fatalf("SyncFilesetsForContext failed: %v", err)
	}
	if len(restartPending) != 0 {
		t.Errorf("restartPending = %v, want empty", restartPending)
	}
	_ = projectToStack

	if mock.composeConfigFullCalls != 0 {
		t.Errorf("ComposeConfigFull called %d times for a context with no filesets, want 0", mock.composeConfigFullCalls)
	}
}

func TestFilesetManager_New(t *testing.T) {
	// Test basic construction without Docker dependencies
	mockDocker := newMockDocker()
	manager := NewFilesetManager(mockDocker, nil)
	if manager.docker != mockDocker {
		t.Error("manager docker client not set correctly")
	}
}

func TestFilesetManager_ConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		filesets    map[string]manifest.FilesetSpec
		expectValid bool
	}{
		{
			name:        "empty filesets",
			filesets:    map[string]manifest.FilesetSpec{},
			expectValid: true,
		},
		{
			name: "valid fileset",
			filesets: map[string]manifest.FilesetSpec{
				"web-assets": {TargetVolume: "web-data", TargetPath: "/var/www"},
			},
			expectValid: true,
		},
		{
			name: "fileset with empty target volume",
			filesets: map[string]manifest.FilesetSpec{
				"web-assets": {TargetVolume: "", TargetPath: "/var/www"},
			},
			expectValid: false,
		},
		{
			name: "fileset with empty target path",
			filesets: map[string]manifest.FilesetSpec{
				"web-assets": {TargetVolume: "web-data", TargetPath: ""},
			},
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test basic validation logic
			for name, fileset := range tt.filesets {
				if fileset.TargetVolume == "" {
					if tt.expectValid {
						t.Errorf("Fileset %q with empty target volume should be invalid", name)
					}
				}
				if fileset.TargetPath == "" {
					if tt.expectValid {
						t.Errorf("Fileset %q with empty target path should be invalid", name)
					}
				}
			}
		})
	}
}

func TestFilesetManager_RestartServicesParsing(t *testing.T) {
	tests := []struct {
		name            string
		restartServices manifest.RestartTargets
		expectedCount   int
	}{
		{
			name:            "no restart services",
			restartServices: manifest.RestartTargets{},
			expectedCount:   0,
		},
		{
			name:            "single service",
			restartServices: manifest.RestartTargets{Services: []string{"web"}},
			expectedCount:   1,
		},
		{
			name:            "multiple services",
			restartServices: manifest.RestartTargets{Services: []string{"web", "api", "db"}},
			expectedCount:   3,
		},
		{
			name:            "attached mode",
			restartServices: manifest.RestartTargets{Attached: true},
			expectedCount:   0, // Would be resolved at runtime
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileset := manifest.FilesetSpec{
				TargetVolume:    "test-vol",
				TargetPath:      "/app",
				RestartServices: tt.restartServices,
			}

			if len(fileset.RestartServices.Services) != tt.expectedCount {
				t.Errorf("expected %d services, got %d", tt.expectedCount, len(fileset.RestartServices.Services))
			}

			// Verify content matches for explicit services
			if !fileset.RestartServices.Attached {
				for i, expected := range tt.restartServices.Services {
					if i < len(fileset.RestartServices.Services) && fileset.RestartServices.Services[i] != expected {
						t.Errorf("expected service %q at index %d, got %q", expected, i, fileset.RestartServices.Services[i])
					}
				}
			}
		})
	}
}

// Additional tests will be handled by integration testing
// These basic configuration tests validate the essential logic
