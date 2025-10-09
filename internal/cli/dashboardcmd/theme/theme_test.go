package theme

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/lipgloss/v2"
)

func TestColorPalette(t *testing.T) {
	tests := map[string]string{
		"FgBase":      fmt.Sprint(FgBase),
		"FgHalfMuted": fmt.Sprint(FgHalfMuted),
		"FgMuted":     fmt.Sprint(FgMuted),
		"FgSubtle":    fmt.Sprint(FgSubtle),
		"FgSelected":  fmt.Sprint(FgSelected),
		"BgBase":      fmt.Sprint(BgBase),
		"Success":     fmt.Sprint(Success),
		"Error":       fmt.Sprint(Error),
		"Warning":     fmt.Sprint(Warning),
		"Info":        fmt.Sprint(Info),
		"Primary":     fmt.Sprint(Primary),
		"Secondary":   fmt.Sprint(Secondary),
		"Tertiary":    fmt.Sprint(Tertiary),
		"Accent":      fmt.Sprint(Accent),
	}

	expect := map[string]string{
		"FgBase":      fmt.Sprint(lipgloss.Color("#C8D3F5")),
		"FgHalfMuted": fmt.Sprint(lipgloss.Color("#828BB8")),
		"FgMuted":     fmt.Sprint(lipgloss.Color("#444A73")),
		"FgSubtle":    fmt.Sprint(lipgloss.Color("#313657")),
		"FgSelected":  fmt.Sprint(lipgloss.Color("#F1EFEF")),
		"BgBase":      fmt.Sprint(lipgloss.Color("#222436")),
		"Success":     fmt.Sprint(lipgloss.Color("#12C78F")),
		"Error":       fmt.Sprint(lipgloss.Color("#EB4268")),
		"Warning":     fmt.Sprint(lipgloss.Color("#E8FE96")),
		"Info":        fmt.Sprint(lipgloss.Color("#00A4FF")),
		"Primary":     fmt.Sprint(lipgloss.Color("#4776FF")),
		"Secondary":   fmt.Sprint(lipgloss.Color("#FF60FF")),
		"Tertiary":    fmt.Sprint(lipgloss.Color("#68FFD6")),
		"Accent":      fmt.Sprint(lipgloss.Color("#E8FE96")),
	}

	for name, got := range tests {
		if want := expect[name]; got != want {
			t.Fatalf("%s: expected %s, got %s", name, want, got)
		}
	}
}
