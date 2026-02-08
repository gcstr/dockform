package dockercli

import (
	"testing"

	"github.com/gcstr/dockform/internal/manifest"
)

func TestNewClientFactory(t *testing.T) {
	factory := NewClientFactory()
	if factory.clients == nil {
		t.Fatal("expected initialized clients map")
	}
}

func TestDefaultClientFactory_GetClient_CachesClients(t *testing.T) {
	factory := NewClientFactory()

	// First call creates client
	client1 := factory.GetClient("default", "myapp")
	if client1 == nil {
		t.Fatal("expected non-nil client")
	}

	// Second call returns same client
	client2 := factory.GetClient("default", "myapp")
	if client1 != client2 {
		t.Error("expected same client instance from cache")
	}

	// Different context returns different client
	client3 := factory.GetClient("other", "myapp")
	if client3 == client1 {
		t.Error("expected different client for different context")
	}

	// Different identifier returns different client
	client4 := factory.GetClient("default", "other-app")
	if client4 == client1 {
		t.Error("expected different client for different identifier")
	}
}

func TestDefaultClientFactory_GetClientForContext(t *testing.T) {
	factory := NewClientFactory()
	cfg := &manifest.Config{
		Identifier: "testapp",
		Contexts: map[string]manifest.ContextConfig{
			"prod": {},
		},
	}

	// Valid context returns client
	client := factory.GetClientForContext("prod", cfg)
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Fallback for unknown context still works
	clientUnknown := factory.GetClientForContext("unknown", cfg)
	if clientUnknown == nil {
		t.Fatal("expected non-nil client for unknown context")
	}
}

func TestDefaultClientFactory_GetAllClients(t *testing.T) {
	factory := NewClientFactory()

	// Initially empty
	clients := factory.GetAllClients()
	if len(clients) != 0 {
		t.Errorf("expected 0 clients, got %d", len(clients))
	}

	// Add some clients
	factory.GetClient("ctx1", "id1")
	factory.GetClient("ctx2", "id2")

	clients = factory.GetAllClients()
	if len(clients) != 2 {
		t.Errorf("expected 2 clients, got %d", len(clients))
	}

	// Verify returned map is a copy
	clients["test"] = nil
	if len(factory.GetAllClients()) != 2 {
		t.Error("expected GetAllClients to return a copy")
	}
}

func TestCacheKey(t *testing.T) {
	tests := []struct {
		context    string
		identifier string
		want       string
	}{
		{"default", "app", "default:app"},
		{"", "app", ":app"},
		{"ctx", "", "ctx:"},
		{"", "", ":"},
	}

	for _, tt := range tests {
		got := cacheKey(tt.context, tt.identifier)
		if got != tt.want {
			t.Errorf("cacheKey(%q, %q) = %q, want %q", tt.context, tt.identifier, got, tt.want)
		}
	}
}
