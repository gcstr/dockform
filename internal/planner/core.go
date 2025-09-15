package planner

import (
	"github.com/gcstr/dockform/internal/ui"
)

// Planner creates a plan comparing desired and current docker state.
type Planner struct {
	docker   DockerClient
	pr       ui.Printer
	prog     *ui.Progress
	parallel bool
}

func New() *Planner { return &Planner{parallel: true} }

func NewWithDocker(client DockerClient) *Planner { return &Planner{docker: client, parallel: true} }

// WithPrinter sets the output printer for user-facing messages during apply/prune.
func (p *Planner) WithPrinter(pr ui.Printer) *Planner {
	p.pr = pr
	return p
}

// WithProgress sets a progress bar to report stepwise progress during apply.
func (p *Planner) WithProgress(pb *ui.Progress) *Planner {
	p.prog = pb
	return p
}

// WithParallel enables or disables parallel processing for plan building.
func (p *Planner) WithParallel(enabled bool) *Planner {
	p.parallel = enabled
	return p
}
