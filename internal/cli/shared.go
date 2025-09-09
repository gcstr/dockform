package cli

import (
	"fmt"
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

	// Render with a simple left border, no header
	lines := []string{
		fmt.Sprintf("│ Context: %s", ui.Italic(ctxName)),
		fmt.Sprintf("│ Identifier: %s", ui.Italic(cfg.Docker.Identifier)),
	}
	pr.Plain("\n%s", strings.Join(lines, "\n"))
}
