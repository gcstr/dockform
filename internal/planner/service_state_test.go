package planner

import (
	"context"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

func TestServiceStateDetector_BuildInlineEnv(t *testing.T) {
	detector := NewServiceStateDetector(nil)

	app := manifest.Stack{
		EnvInline: []string{"FOO=bar", "BAZ=qux"},
	}

	result := detector.BuildInlineEnv(context.Background(), app, nil)

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
