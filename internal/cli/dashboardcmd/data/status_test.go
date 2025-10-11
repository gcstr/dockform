package data

import (
	"context"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
)

func TestFormatStatusLine_RunningHealthy(t *testing.T) {
	color, text := FormatStatusLine("running", "Up 3m (healthy)")
	if color != "success" {
		t.Fatalf("expected success color, got %q", color)
	}
	if text != "Up 3m (healthy)" {
		t.Fatalf("unexpected status text: %q", text)
	}
}

func TestFormatStatusLine_RunningStarting(t *testing.T) {
	color, _ := FormatStatusLine("running", "Starting (starting)")
	if color != "warning" {
		t.Fatalf("expected warning when container is starting, got %q", color)
	}
}

func TestFormatStatusLine_Defaults(t *testing.T) {
	if c, _ := FormatStatusLine("restarting", "Restarting"); c != "warning" {
		t.Fatalf("expected warning for restarting state, got %q", c)
	}
	if c, _ := FormatStatusLine("exited", "Exited (0)"); c != "error" {
		t.Fatalf("expected error for exited state, got %q", c)
	}
}

func TestColorStyle_ProducesANSI(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"success", "success"},
		{"warning", "warning"},
		{"error", "error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ColorStyle(tc.key, "●")
			if !strings.HasPrefix(got, "\x1b[") || !strings.HasSuffix(got, "\x1b[0m") {
				t.Fatalf("expected ANSI colored string, got %q", got)
			}
			if !strings.Contains(got, "●") {
				t.Fatalf("expected bullet to be preserved, got %q", got)
			}
		})
	}
}

func TestStatusProviderDockerAccessors(t *testing.T) {
	client := dockercli.New("")
	sp := NewStatusProvider(client, "  demo  ")
	if sp.Docker() != client {
		t.Fatalf("expected Docker to return the provided client")
	}
	if sp.identifier != "demo" {
		t.Fatalf("expected identifier to be trimmed, got %q", sp.identifier)
	}
}

func TestResolveContainerNamePrefersExplicitName(t *testing.T) {
	client := dockercli.New("")
	sp := NewStatusProvider(client, "")
	name, err := sp.ResolveContainerName(context.Background(), "stack", ServiceSummary{ContainerName: " demo "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "demo" {
		t.Fatalf("expected name to be trimmed, got %q", name)
	}
}
