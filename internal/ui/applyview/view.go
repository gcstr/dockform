package applyview

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/gcstr/dockform/internal/planner"
)

// spinnerFrames mirrors internal/ui/rollinglog.go:272 so every dockform spinner
// animates identically.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var (
	styleDone    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleFail    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("69"))
	stylePending = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// marker is the leading glyph for a line's state.
func (m Model) marker(it *Item) string {
	switch it.State {
	case planner.StateDone:
		return styleDone.Render("✔")
	case planner.StateFailed:
		return styleFail.Render("✖")
	case planner.StateRunning:
		return styleRunning.Render(spinnerFrames[m.frame%len(spinnerFrames)])
	default:
		return stylePending.Render("·")
	}
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// status is the right-hand text for a line.
func (it *Item) status() string {
	switch it.State {
	case planner.StateDone:
		return it.Result
	case planner.StateFailed:
		if it.Err != nil {
			return it.Err.Error()
		}
		return "failed"
	case planner.StateRunning:
		if it.Detail != "" {
			return it.Detail
		}
		return it.Verb + "…"
	default:
		return "pending"
	}
}

// summary renders a collapsed group: how many lines, the shared result verb, and
// the wall time the group took.
func (g *Group) summary() (count int, result string, total time.Duration) {
	for _, it := range g.items {
		count++
		total += it.elapsed
		if result == "" && it.Result != "" {
			result = it.Result
		}
	}
	return count, result, total
}

// plural picks singular or plural based on n, so counts next to a word never
// read as grammatically wrong (e.g. "1 contexts").
func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func (m Model) View() string {
	var b strings.Builder

	done, total := m.Counts()

	if m.state == stateRunning {
		contexts := m.contextCount()
		fmt.Fprintf(&b, "Applying %d %s · %d %s\n\n",
			total, plural(total, "change", "changes"), contexts, plural(contexts, "context", "contexts"))
	} else {
		fmt.Fprintf(&b, "Applied %d/%d %s in %s", done, total, plural(total, "change", "changes"), formatDuration(m.totalElapsed()))
		if n := len(m.Failures()); n > 0 {
			fmt.Fprintf(&b, " · %d failed", n)
		}
		b.WriteString("\n\n")
	}

	lastContext := ""
	for _, g := range m.groups {
		if g.Context != lastContext {
			b.WriteString(" " + g.Context + "\n")
			lastContext = g.Context
		}

		if g.collapsed() {
			count, result, groupTotal := g.summary()
			fmt.Fprintf(&b, "  %s %-16s %2d %-10s %8s\n",
				styleDone.Render("✔"), g.Title, count, result, formatDuration(groupTotal))
			continue
		}

		fmt.Fprintf(&b, "  %s %s\n", m.groupMarker(g), g.Title)
		for _, it := range g.items {
			indent := "     "
			if it.Ref.Parent != "" {
				indent = "       "
			}
			name := it.Ref.Name
			if it.Discovered {
				name += " (discovered)"
			}
			line := fmt.Sprintf("%s%s %-22s %s %s",
				indent, m.marker(it), name, it.status(), styleDim.Render(formatDuration(it.elapsed)))
			b.WriteString(strings.TrimRight(line, " ") + "\n")
		}
	}

	b.WriteString("\n")

	if m.state == stateFinal {
		if failures := m.Failures(); len(failures) > 0 {
			for _, it := range failures {
				fmt.Fprintf(&b, "  %s %s %s/%s  %s\n",
					styleFail.Render("✖"), it.Ref.Context, groupTitle(it.Ref.Type), it.Ref.Name, it.status())
			}
			b.WriteString("\n")
		}
		if m.logPath != "" {
			b.WriteString("  log: " + m.logPath + "\n")
		}
		// Clear anything the live view left below.
		b.WriteString("\x1b[0J")
		return b.String()
	}

	fmt.Fprintf(&b, " %d/%d · %s\n", done, total, formatDuration(m.totalElapsed()))

	out := b.String()
	if m.width > 1 {
		var trimmed strings.Builder
		for _, line := range strings.Split(out, "\n") {
			trimmed.WriteString(ansi.Truncate(line, m.width-1, ""))
			trimmed.WriteByte('\n')
		}
		out = strings.TrimSuffix(trimmed.String(), "\n")
	}
	return out
}

// groupMarker shows a spinner while any line in the group is running.
func (m Model) groupMarker(g *Group) string {
	for _, it := range g.items {
		if it.State == planner.StateRunning {
			return styleRunning.Render(spinnerFrames[m.frame%len(spinnerFrames)])
		}
	}
	if g.anyFailed() {
		return styleFail.Render("✖")
	}
	return stylePending.Render("·")
}

func (m Model) contextCount() int {
	seen := map[string]struct{}{}
	for _, g := range m.groups {
		seen[g.Context] = struct{}{}
	}
	return len(seen)
}

func (m Model) totalElapsed() time.Duration {
	if m.started.IsZero() {
		return 0
	}
	return m.now().Sub(m.started)
}
