package applyview

import (
	"fmt"
	"sort"
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

// formatDuration renders elapsed time for a line. Anything under 50ms is
// dropped entirely rather than shown: at that resolution it is noise, not
// information, and a bare "0ms" would read no better than the "0.0s" this
// exists to avoid. Between 50ms and 1s it renders in whole milliseconds
// (e.g. "40ms") since a tenth-of-a-second reading has no useful precision
// there. Both applyview's own renderer and Plain (which reuses this func)
// share the same thresholds, so the two never disagree on the same run.
func formatDuration(d time.Duration) string {
	if d < 50*time.Millisecond {
		return ""
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
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

// depthZeroCount returns how many depth-0 (top-level) lines a group holds. A
// service nested under a stack (Ref.Parent set) shares its parent stack's own
// result and timing — it never gets an independent Start — so counting it
// separately would double-count the same unit of work. For a Stacks group this
// means the count reflects how many STACKS were touched, not stacks-plus-
// services; the pending and done branches of a collapsed group both call this
// so they can never disagree with each other the way "9 pending" vs. "3
// stacks" once did.
func (g *Group) depthZeroCount() int {
	n := 0
	for _, it := range g.items {
		if it.Ref.Parent == "" {
			n++
		}
	}
	return n
}

// summary renders a collapsed, finished group: a breakdown of its depth-0
// lines by result verb — the dominant one first, with any remainder called
// out (e.g. "9 started, 2 up to date") rather than silently reporting the
// count under whichever verb happened to belong to the first item — and the
// wall-clock span that work actually took, computed as max(end)-min(start)
// across those same lines rather than a sum of their individual elapsed
// times. Apply runs contexts, and the items within one group, in parallel; a
// sum overstates the group's real duration, often by a lot, once any two
// items overlap.
//
// Only depth-0 items participate, for the same reason depthZeroCount only
// counts them: a nested service's Result and elapsed are inherited from its
// stack's own Finish, so folding it in here would both double-count the verb
// and widen the time window with a duplicate of an interval already counted.
func (g *Group) summary() (breakdown string, total time.Duration) {
	counts := map[string]int{}
	var order []string
	var minStart, maxEnd time.Time
	for _, it := range g.items {
		if it.Ref.Parent != "" {
			continue
		}
		verb := it.Result
		if verb != "" {
			if counts[verb] == 0 {
				order = append(order, verb)
			}
			counts[verb]++
		}
		if it.started.IsZero() {
			continue
		}
		if minStart.IsZero() || it.started.Before(minStart) {
			minStart = it.started
		}
		if end := it.started.Add(it.elapsed); end.After(maxEnd) {
			maxEnd = end
		}
	}
	if !minStart.IsZero() {
		total = maxEnd.Sub(minStart)
	}
	return renderBreakdown(order, counts), total
}

// renderBreakdown turns a verb histogram into "<n> <verb>[, <n> <verb>...]",
// ranked by count so the dominant result leads and the remainder trails —
// order is a tie-breaker only (first-seen order among equally common verbs),
// never the primary sort key the old first-non-empty-Result logic used.
func renderBreakdown(order []string, counts map[string]int) string {
	if len(order) == 0 {
		return ""
	}
	sorted := append([]string(nil), order...)
	sort.SliceStable(sorted, func(i, j int) bool { return counts[sorted[i]] > counts[sorted[j]] })
	parts := make([]string, 0, len(sorted))
	for _, verb := range sorted {
		parts = append(parts, fmt.Sprintf("%d %s", counts[verb], verb))
	}
	return strings.Join(parts, ", ")
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

// padDisplayGap is padDisplay's counterpart for a boundary that must always
// carry visible separation on the far side, even once s has already reached
// or exceeded width. It exists for the item line's name/status boundary:
// dockform resource names routinely run past itemNameWidth (22 cells), and
// padDisplay's "already at width, leave it alone" rule then left the status
// text butted directly against the name with no space at all, e.g.
// "twentythree_chars_long1syncing…". Every other padDisplay call site already
// has a literal separator after it regardless of width, so only this one
// needs the guarantee.
func padDisplayGap(s string, width int) string {
	if w := ansi.StringWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s + " "
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

// bodyLine is one rendered line of the resource list, tagged with whether it
// belongs to work that is currently running. The height window centres on the
// active region, so a long run shows what is happening rather than the top of a
// list that may be a hundred lines long.
type bodyLine struct {
	text   string
	active bool
}

// bodyLines renders the context/group/item lines, without the header or footer.
func (m Model) bodyLines() []bodyLine {
	var out []bodyLine
	// Emit each context's groups together, so a group created LATE — a discovered
	// restart, or a fileset that only announces itself once apply reaches it —
	// joins its context's block instead of repeating the context header further
	// down. Relying on m.groups being contiguous by context was what printed
	// "hetzner-two" twice on a real three-host run.
	seen := map[string]bool{}
	var order []string
	for _, g := range m.groups {
		if !seen[g.Context] {
			seen[g.Context] = true
			order = append(order, g.Context)
		}
	}
	ordered := make([]*Group, 0, len(m.groups))
	for _, ctx := range order {
		for _, g := range m.groups {
			if g.Context == ctx {
				ordered = append(ordered, g)
			}
		}
	}

	lastContext := ""
	for _, g := range ordered {
		// An empty group has nothing to say; dropItem removes them, this is the
		// belt to that braces.
		if len(g.items) == 0 {
			continue
		}
		running := false
		for _, it := range g.items {
			if it.State == planner.StateRunning {
				running = true
				break
			}
		}

		if g.Context != lastContext {
			out = append(out, bodyLine{text: " " + g.Context, active: running})
			lastContext = g.Context
		}

		if g.collapsed() {
			if !g.started() {
				// Not started yet: there is no result to report and no duration
				// to show, so say how much is waiting rather than pretend. Counts
				// depth-0 lines only, so this agrees with the done branch below —
				// a Stacks group of 3 stacks (each with 2 services) says "3
				// pending", never "9 pending".
				out = append(out, bodyLine{text: fmt.Sprintf("  %s %-16s %s",
					stylePending.Render("·"), g.Title, fmt.Sprintf("%d pending", g.depthZeroCount()))})
				continue
			}
			breakdown, groupTotal := g.summary()
			out = append(out, bodyLine{text: strings.TrimRight(fmt.Sprintf("  %s %-16s %s  %s",
				styleDone.Render("✔"), g.Title, padDisplay(breakdown, minStatusWidth), formatDuration(groupTotal)), " ")})
			continue
		}

		out = append(out, bodyLine{text: fmt.Sprintf("  %s %s", m.groupMarker(g), g.Title), active: running})
		for _, it := range g.items {
			indent := itemIndent
			if it.Ref.Parent != "" {
				indent = childIndent
			}
			name := it.Ref.Name
			if it.Discovered {
				name += " (discovered)"
			}
			prefix := padDisplayGap(indent+m.marker(it)+" "+name, statusColumn)
			status := padDisplay(m.itemStatus(it), minStatusWidth)
			line := prefix + status + "  " + styleDim.Render(formatDuration(it.elapsed))
			out = append(out, bodyLine{
				text:   strings.TrimRight(line, " "),
				active: it.State == planner.StateRunning,
			})
		}
	}
	return out
}

// fitBody windows lines to budget, keeping the active region visible and saying
// how much it dropped.
//
// This exists because Bubble Tea's renderer keeps only the LAST height lines of
// whatever it is handed (standard_renderer.go), silently discarding everything
// above — on a real manifest that is the header and the first hosts, for the whole
// early phase of a run. It is the same failure mode that keeps the PLAN output off
// the TUI entirely (see the comment in internal/cli/applycmd/new.go).
func fitBody(lines []bodyLine, budget int) []string {
	if budget <= 0 {
		return nil
	}
	if len(lines) <= budget {
		out := make([]string, 0, len(lines))
		for _, l := range lines {
			out = append(out, l.text)
		}
		return out
	}

	first, last := 0, 0
	for i, l := range lines {
		if l.active {
			if first == 0 && !lines[0].active {
				first = i
			}
			last = i
		}
	}
	if last < first {
		last = first
	}

	centre := (first + last) / 2

	// Below three lines the budget cannot hold both markers AND anything to look
	// at, so drop the markers and keep the content: a marker describing what you
	// cannot see is worth less than the one line you can. Without this the settle
	// loop's w<1 clamp forces a one-line window that the markers then push over
	// budget — emitting budget+2 lines, which is the overflow this whole function
	// exists to prevent.
	if budget < 3 {
		start := centre - budget/2
		if start+budget > len(lines) {
			start = len(lines) - budget
		}
		if start < 0 {
			start = 0
		}
		out := make([]string, 0, budget)
		for _, l := range lines[start : start+budget] {
			out = append(out, l.text)
		}
		return out
	}

	// Each marker costs a line of the budget, and whether we need one depends on
	// where the window lands — so settle the two together.
	window := budget
	start := 0
	for pass := 0; pass < 3; pass++ {
		start = centre - window/2
		if start+window > len(lines) {
			start = len(lines) - window
		}
		if start < 0 {
			start = 0
		}
		w := budget
		if start > 0 {
			w--
		}
		if start+window < len(lines) {
			w--
		}
		if w < 1 {
			w = 1
		}
		if w == window {
			break
		}
		window = w
	}
	if start+window > len(lines) {
		window = len(lines) - start
	}

	var out []string
	if start > 0 {
		out = append(out, styleDim.Render(fmt.Sprintf("  ↑ %d more", start)))
	}
	for _, l := range lines[start : start+window] {
		out = append(out, l.text)
	}
	if rest := len(lines) - (start + window); rest > 0 {
		out = append(out, styleDim.Render(fmt.Sprintf("  ↓ %d more", rest)))
	}
	return out
}

// The view counts RESOURCES, deliberately not the "changes" the plan footer
// counts. A fileset is one line here — 40 changed files sync as a single tar
// extract, so there is no state where "12 of 40 files are applied" — while the
// plan counts each file, which is the right answer to "what will change" before
// you approve it. The two numbers measure different things on purpose; naming
// them differently is what stops "Plan: 46 to create" / "Applying 7 changes"
// from reading as a bug.
func (m Model) View() string {
	var head, foot strings.Builder

	done, total := m.Counts()

	if m.state == stateRunning {
		contexts := m.contextCount()
		fmt.Fprintf(&head, "Applying %d %s · %d %s\n\n",
			total, plural(total, "resource", "resources"), contexts, plural(contexts, "context", "contexts"))
	} else {
		fmt.Fprintf(&head, "Applied %d/%d %s in %s", done, total, plural(total, "resource", "resources"), formatDuration(m.totalElapsed()))
		if n := len(m.Failures()); n > 0 {
			fmt.Fprintf(&head, " · %d failed", n)
		}
		head.WriteString("\n\n")
	}

	foot.WriteString("\n")
	if m.state == stateFinal {
		if failures := m.Failures(); len(failures) > 0 {
			for _, it := range failures {
				fmt.Fprintf(&foot, "  %s %s %s/%s  %s\n",
					styleFail.Render("✖"), it.Ref.Context, groupTitle(it.Ref.Type), it.Ref.Name, it.status())
			}
			foot.WriteString("\n")
		}
		// Spell out where the rest of the total went. Without this the header
		// reads "Applied 2/5 changes · 1 failed" and leaves three lines
		// unexplained, while the "1 failed" refers to a stack that is not one of
		// the five — an invitation to arithmetic that does not work. These
		// buckets DO sum to the total, and mirror what the plain renderer
		// already prints, so the two views agree about the same run.
		if _, interrupted, notApplied := m.Outcomes(); interrupted > 0 || notApplied > 0 {
			var parts []string
			if notApplied > 0 {
				parts = append(parts, fmt.Sprintf("%d not applied", notApplied))
			}
			if interrupted > 0 {
				parts = append(parts, fmt.Sprintf("%d interrupted", interrupted))
			}
			fmt.Fprintf(&foot, "  %s\n\n", styleDim.Render(strings.Join(parts, " · ")))
		}
		if m.logPath != "" {
			foot.WriteString("  log: " + m.logPath + "\n")
		}
	} else {
		fmt.Fprintf(&foot, " %d/%d · %s\n", done, total, formatDuration(m.totalElapsed()))
	}

	body := m.bodyLines()
	// The header and footer are pinned: they carry the totals and the log path,
	// which are the last things that should fall off the screen. Only the middle
	// is windowed. height is 0 until the first WindowSizeMsg arrives, and that
	// means "unknown", not "zero" — render everything.
	if m.height > 0 {
		budget := m.height - strings.Count(head.String(), "\n") - strings.Count(foot.String(), "\n")
		body = linesOf(fitBody(body, budget))
	}

	var b strings.Builder
	b.WriteString(head.String())
	for _, l := range body {
		b.WriteString(l.text)
		b.WriteByte('\n')
	}
	b.WriteString(foot.String())

	return m.truncated(b.String())
}

// linesOf re-wraps plain strings as bodyLines so View's writer loop stays one
// shape whether or not the body was windowed.
func linesOf(texts []string) []bodyLine {
	out := make([]bodyLine, 0, len(texts))
	for _, t := range texts {
		out = append(out, bodyLine{text: t})
	}
	return out
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
			// The tail is "…" rather than "" so a cut line is visibly cut — an
			// empty tail just stops mid-word (e.g. a failure cause truncated to
			// "...image pull fa") with nothing to tell the reader more was there.
			// No line in this view is meant to end in a space; when truncation
			// lands inside a column's padding it otherwise leaves one behind
			// before the ellipsis.
			lines[i] = strings.TrimRight(ansi.Truncate(line, m.width-1, "…"), " ")
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
