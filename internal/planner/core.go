package planner

import (
	"github.com/gcstr/dockform/internal/ui"
)

// Planner creates a plan comparing desired and current docker state.
type Planner struct {
	docker        DockerClient
	pr            ui.Printer
	spinner       *ui.Spinner
	spinnerPrefix string // Prefix for dynamic spinner labels (e.g., "Applying", "Destroying")
	parallel      bool
}

func New() *Planner { return &Planner{parallel: true} }

func NewWithDocker(client DockerClient) *Planner { return &Planner{docker: client, parallel: true} }

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
