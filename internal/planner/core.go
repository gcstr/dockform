package planner

import (
	"github.com/gcstr/dockform/internal/ui"
)

// Planner creates a plan comparing desired and current docker state.
type Planner struct {
	docker DockerClient
	pr     ui.Printer
	prog   *ui.Progress
}

func New() *Planner { return &Planner{} }

func NewWithDocker(client DockerClient) *Planner { return &Planner{docker: client} }

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
