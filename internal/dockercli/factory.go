package dockercli

import (
	"sync"

	"github.com/gcstr/dockform/internal/manifest"
)

// ClientFactory creates and caches Docker clients for different contexts.
type ClientFactory interface {
	// GetClient returns a Docker client for the specified context name.
	// The identifier is used to scope resource discovery.
	GetClient(contextName, identifier string) *Client

	// GetClientForContext returns a Docker client configured for the specified context.
	GetClientForContext(contextName string, cfg *manifest.Config) *Client
}

// DefaultClientFactory is the standard implementation of ClientFactory.
// It caches clients per context+identifier combination for efficient reuse.
type DefaultClientFactory struct {
	clients map[string]*Client
	mu      sync.RWMutex
}

// NewClientFactory creates a new DefaultClientFactory.
func NewClientFactory() *DefaultClientFactory {
	return &DefaultClientFactory{
		clients: make(map[string]*Client),
	}
}

// cacheKey generates a unique key for the client cache.
func cacheKey(contextName, identifier string) string {
	return contextName + ":" + identifier
}

// GetClient returns a Docker client for the specified context name.
// Clients are cached and reused for the same context+identifier combination.
func (f *DefaultClientFactory) GetClient(contextName, identifier string) *Client {
	key := cacheKey(contextName, identifier)

	// Try read lock first for cached client
	f.mu.RLock()
	if client, ok := f.clients[key]; ok {
		f.mu.RUnlock()
		return client
	}
	f.mu.RUnlock()

	// Upgrade to write lock and create client
	f.mu.Lock()
	defer f.mu.Unlock()

	// Double-check after acquiring write lock
	if client, ok := f.clients[key]; ok {
		return client
	}

	client := New(contextName).WithIdentifier(identifier)
	f.clients[key] = client
	return client
}

// GetClientForContext returns a Docker client configured for the specified context.
func (f *DefaultClientFactory) GetClientForContext(contextName string, cfg *manifest.Config) *Client {
	_, ok := cfg.Contexts[contextName]
	if !ok {
		// Fallback: return a client with context name (shouldn't happen in normal use)
		return f.GetClient(contextName, cfg.Identifier)
	}
	// In the new schema, context name IS the Docker context, and identifier is project-wide
	return f.GetClient(contextName, cfg.Identifier)
}

// GetAllClients returns all cached clients. Useful for cleanup or bulk operations.
func (f *DefaultClientFactory) GetAllClients() map[string]*Client {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make(map[string]*Client, len(f.clients))
	for k, v := range f.clients {
		result[k] = v
	}
	return result
}
