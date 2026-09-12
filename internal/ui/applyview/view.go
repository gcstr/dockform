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
	styleDone        = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleFail        = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleRunning     = lipgloss.NewStyle().Foreground(lipgloss.Color("69"))
	stylePending     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleDim         = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleInterrupted = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

// markerInterrupted marks an item or group that was still StateRunning when the
// run ended: not done (✔), not failed (✖), and not pending (·). It started and
// its outcome is simply unknown because something else cut the run short — a
// third truth the Done/Failed/Pending vocabulary has no glyph for. It never
// animates: a spinner here would claim the run is still making progress on it,
// which is no longer true.
const markerInterrupted = "■"

// marker is the leading glyph for a line's state.
func (m Model) marker(it *Item) string {
	switch it.State {
	case planner.StateDone:
		return styleDone.Render("✔")
	case planner.StateFailed:
		return styleFail.Render("✖")
	case planner.StateRunning:
		if m.state == stateFinal {
			return styleInterrupted.Render(markerInterrupted)
		}
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

// status is the right-hand text for a line, driven only by the item's own
// state. The final-state override for an item that was cut off mid-run lives
// in (Model).itemStatus, which has the m.state this method doesn't.
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

// itemStatus is the status text actually rendered for it. A StateRunning item
// that is still running once the model has reached stateFinal did not
// succeed, fail, or stay pending — it started and was cut off (Ctrl+C, or a
// sibling failing and unwinding the run while this was mid-flight). That is a
// distinct, honest truth from the other three, so it gets its own word.
func (m Model) itemStatus(it *Item) string {
	if m.state == stateFinal && it.State == planner.StateRunning {
		return "interrupted"
	}
	return it.status()
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

// padDisplay right-pads s with spaces until its terminal display width is at
// least width. Width is measured with ansi.StringWidth rather than len/byte
// count, because this view's markers (✔, ✖, ⠋, ·, ■) are multi-byte UTF-8 but
// occupy exactly one terminal column, and ANSI color codes occupy none. A
// string already at or past width is returned unchanged.
func padDisplay(s string, width int) string {
	if w := ansi.StringWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// itemIndent is the left margin for a depth-0 item line: a top-level resource
// such as a volume, a stack, or a fileset.
const (
	itemIndent    = "     " // 5 spaces
	itemNameWidth = 22
)

// childIndent is one nesting level deeper than itemIndent (a service under a
// stack) — derived from itemIndent rather than stated as its own number, so
// the two can never drift apart independently.
const childIndent = itemIndent + "  "

// statusColumn is the fixed display width that every item line's left-hand
// part — indent + marker + space + name — is padded to before the status text
// is appended, regardless of nesting depth. Without this, a depth-1 line's
// status starts two columns later than a depth-0 line's, because its indent is
// two columns wider while the name field stays the same width: exactly the
// staggered-column bug this constant exists to prevent. It is derived once
// from the depth-0 layout below, so a third nesting level added later just
// consumes more of the same pad — it never needs a magic number of its own.
const statusColumn = len(itemIndent) + 1 /* marker */ + 1 /* space */ + itemNameWidth + 1 /* space */

// minStatusWidth is the minimum display width the status text is padded to
// before the duration is appended. It keeps at least two spaces between the
// two (an arbitrary-length error string butting straight up against a
// duration is the exact collision this exists to prevent) and lines up the
// durations of any rows whose status fits within it. A status longer than
// this — a long error message, typically — simply pushes its own duration
// right; that row loses column alignment with its neighbors but the
// separation stays unambiguous, which is what actually matters.
const minStatusWidth = 14

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
			indent := itemIndent
			if it.Ref.Parent != "" {
				indent = childIndent
			}
			name := it.Ref.Name
			if it.Discovered {
				name += " (discovered)"
			}
			prefix := padDisplay(indent+m.marker(it)+" "+name, statusColumn)
			status := padDisplay(m.itemStatus(it), minStatusWidth)
			line := prefix + status + "  " + styleDim.Render(formatDuration(it.elapsed))
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
	} else {
		fmt.Fprintf(&b, " %d/%d · %s\n", done, total, formatDuration(m.totalElapsed()))
	}

	return m.truncated(b.String())
}

// truncated applies the same width protection to every line this view emits,
// in both stateRunning and stateFinal: a character landing in the terminal's
// last column triggers pending autowrap and breaks Bubble Tea's cursor-up line
// accounting (see internal/ui/rollinglog.go). The live view redraws inline on
// every tick; the final frame is rendered through that exact same inline path
// one last time before tea.Quit takes effect, so it needs the identical
// protection — returning early before this ran was Finding 1.
//
// The trailing "\x1b[0J" erase-to-end-of-screen that stateFinal appends is a
// control sequence, not content: it carries no printable cells, so
// ansi.StringWidth measures it at 0 and ansi.Truncate (which no-ops whenever a
// line is already within budget) always returns it byte-for-byte unchanged,
// regardless of how narrow width is. It is appended here, after the
// line-by-line truncation pass, so it is never itself a candidate for
// truncation in the first place.
func (m Model) truncated(out string) string {
	if m.width > 1 {
		lines := strings.Split(out, "\n")
		for i, line := range lines {
			// No line in this view is meant to end in a space; when truncation
			// lands inside a column's padding it otherwise leaves one behind.
			lines[i] = strings.TrimRight(ansi.Truncate(line, m.width-1, ""), " ")
		}
		out = strings.Join(lines, "\n")
	}
	if m.state == stateFinal {
		out += "\x1b[0J"
	}
	return out
}

// groupMarker is the leading glyph for a group's header line.
func (m Model) groupMarker(g *Group) string {
	if m.state == stateFinal {
		// No spinner in the final frame: nothing is still running. A group
		// holding a failure reports that first — it is the more urgent,
		// conclusive fact — then a group left with a cut-off item reports
		// that honestly too, rather than freezing the spinner it last had.
		if g.anyFailed() {
			return styleFail.Render("✖")
		}
		for _, it := range g.items {
			if it.State == planner.StateRunning {
				return styleInterrupted.Render(markerInterrupted)
			}
		}
		return stylePending.Render("·")
	}
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
