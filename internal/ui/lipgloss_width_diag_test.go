package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestTruncOneRowANSIWithStyles verifies that truncOneRowANSI correctly handles
// strings built by displayLogger.emit() with real lipgloss ANSI escape codes —
// i.e., the truncated result has the right visual width and no unclosed sequences
// that would leak style state into subsequent terminal output.
func TestTruncOneRowANSIWithStyles(t *testing.T) {
	// Force TrueColor so lipgloss emits actual ANSI codes regardless of TTY.
	r := lipgloss.NewRenderer(os.Stderr)
	r.SetColorProfile(termenv.TrueColor)

	displayStyleKey := r.NewStyle().Faint(true)
	lvlStyle := r.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))

	var sb strings.Builder
	sb.WriteString("15:04:05")
	sb.WriteByte(' ')
	sb.WriteString(lvlStyle.Render("INFO"))
	sb.WriteByte(' ')
	sb.WriteString("docker_exec")
	sb.WriteByte(' ')
	sb.WriteString("docker_exec(started)")
	sb.WriteByte(' ')
	sb.WriteString(displayStyleKey.Render("component="))
	sb.WriteString("dockercli")
	sb.WriteByte(' ')
	sb.WriteString(displayStyleKey.Render("resource_kind="))
	sb.WriteString("process")

	s := sb.String()

	// Verify the string contains real ANSI codes (confirming TrueColor forced).
	if !strings.Contains(s, "\x1b[") {
		t.Fatalf("expected ANSI codes in styled string, got plain text: %q", s)
	}

	// Visual width must equal the plain-text equivalent.
	plain := "15:04:05 INFO docker_exec docker_exec(started) component=dockercli resource_kind=process"
	if got := lipgloss.Width(s); got != len(plain) {
		t.Fatalf("lipgloss.Width = %d, want %d", got, len(plain))
	}

	// Truncate to width=80: limit = 78. Result must be ≤ 78 visual cells.
	truncated := truncOneRowANSI(s, 80)
	if w := lipgloss.Width(truncated); w > 78 {
		t.Errorf("truncated line too wide: %d > 78", w)
	}

	// The truncated string must not end with an unclosed ANSI SGR sequence.
	// A properly truncated string from ansi.Truncate closes open sequences,
	// so the last ANSI code should be a reset (\x1b[0m or \x1b[m).
	if strings.Contains(truncated, "\x1b[") {
		// Has ANSI codes — last one should be a reset.
		if !strings.HasSuffix(truncated, "\x1b[0m") && !strings.HasSuffix(truncated, "\x1b[m") {
			t.Errorf("truncated ANSI string does not end with reset, last bytes: %q",
				truncated[max(0, len(truncated)-10):])
		}
	}
}
