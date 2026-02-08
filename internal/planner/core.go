package planner

import (
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
)

// Planner creates a plan comparing desired and current docker state.
// For multi-context support, it uses a ClientFactory to get clients per context.
type Planner struct {
	// Single docker client (for backward compatibility and single-context operations)
	docker DockerClient

	// Client factory for multi-context operations
	factory *dockercli.DefaultClientFactory

	pr            ui.Printer
	spinner       *ui.Spinner
	spinnerPrefix string // Prefix for dynamic spinner labels (e.g., "Applying", "Destroying")
	parallel      bool
}

func New() *Planner { return &Planner{parallel: true} }

func NewWithDocker(client DockerClient) *Planner { return &Planner{docker: client, parallel: true} }

// NewWithFactory creates a planner using a client factory for multi-context support.
func NewWithFactory(factory *dockercli.DefaultClientFactory) *Planner {
	return &Planner{factory: factory, parallel: true}
}

// WithPrinter sets the output printer for user-facing messages during apply/prune.
func (p *Planner) WithPrinter(pr ui.Printer) *Planner {
	p.pr = pr
	return p
}

// WithSpinner sets a spinner to show current task during apply, with a prefix for dynamic labels.
func (p *Planner) WithSpinner(s *ui.Spinner, prefix string) *Planner {
	p.spinner = s
	p.spinnerPrefix = prefix
	return p
}

// WithParallel enables or disables parallel processing for plan building.
func (p *Planner) WithParallel(enabled bool) *Planner {
	p.parallel = enabled
	return p
}

// getClientForContext returns the Docker client for a specific context.
// It first checks if a factory is configured, then falls back to the single client.
func (p *Planner) getClientForContext(contextName string, cfg *manifest.Config) DockerClient {
	if p.factory != nil {
		return p.factory.GetClientForContext(contextName, cfg)
	}
	// Fallback to single client for backward compatibility
	return p.docker
}

