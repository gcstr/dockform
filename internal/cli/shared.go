package cli

import (
	"strings"

	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
)

// displayDockerInfo shows the Docker context and identifier information
func displayDockerInfo(pr ui.Printer, cfg *manifest.Config) {
	ctxName := strings.TrimSpace(cfg.Docker.Context)
	if ctxName == "" {
		ctxName = "default"
	}

	sections := []ui.Section{
		{
			Title: "Docker",
			Items: []ui.DiffLine{
				ui.Line(ui.Info, "Context: %s", ctxName),
				ui.Line(ui.Info, "Identifier: %s", cfg.Docker.Identifier),
			},
		},
	}
	pr.Plain("\n%s", strings.TrimRight(ui.RenderSectionedList(sections), "\n"))
}
