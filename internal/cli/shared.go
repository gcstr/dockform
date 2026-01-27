package cli

import (
	"fmt"
	"strings"

	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
)

// displayDockerInfo shows the daemon configuration information for multi-daemon schema.
func displayDockerInfo(pr ui.Printer, cfg *manifest.Config) {
	if len(cfg.Daemons) == 0 {
		pr.Plain("\n│ No daemons configured")
		return
	}

	var lines []string
	lines = append(lines, "")
	for name, daemon := range cfg.Daemons {
		ctxName := strings.TrimSpace(daemon.Context)
		if ctxName == "" {
			ctxName = "default"
		}
		lines = append(lines, fmt.Sprintf("│ Daemon: %s", ui.Italic(name)))
		lines = append(lines, fmt.Sprintf("│   Context: %s", ui.Italic(ctxName)))
		if daemon.Identifier != "" {
			lines = append(lines, fmt.Sprintf("│   Identifier: %s", ui.Italic(daemon.Identifier)))
		}
	}
	pr.Plain("%s", strings.Join(lines, "\n"))
}
