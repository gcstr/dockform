package cli

import (
	"fmt"
	"strings"

	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
)

// displayDockerInfo shows the daemon configuration information for multi-daemon schema.
func displayDockerInfo(pr ui.Printer, cfg *manifest.Config) {
	if len(cfg.Contexts) == 0 {
		pr.Plain("\n│ No daemons configured")
		return
	}

	var lines []string
	lines = append(lines, "")
	for name := range cfg.Contexts {
		lines = append(lines, fmt.Sprintf("│ Context: %s", ui.Italic(name)))
	}
	if cfg.Identifier != "" {
		lines = append(lines, fmt.Sprintf("│ Identifier: %s", ui.Italic(cfg.Identifier)))
	}
	pr.Plain("%s", strings.Join(lines, "\n"))
}
