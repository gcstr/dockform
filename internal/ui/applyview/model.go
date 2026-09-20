package applyview

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gcstr/dockform/internal/planner"
)

const tickDelay = 100 * time.Millisecond

// Item is one line in the view.
type Item struct {
	Ref        planner.ResourceRef
	State      planner.ResourceState
	Verb       string
	Detail     string
	Percent    int
	HasPercent bool
	Result     string
	Err        error
	Discovered bool

	started time.Time
	elapsed time.Duration
}

// Elapsed is how long the item ran. Zero until it finishes or fails.
func (i *Item) Elapsed() time.Duration { return i.elapsed }

// Group is one titled block of lines within a context.
type Group struct {
	Context string
	Title   string
	items   []*Item
}

// Items exposes the group's lines in order, for the renderer.
func (g *Group) Items() []*Item { return g.items }

// terminal reports whether every line in the group has finished or failed.
func (g *Group) terminal() bool {
	if len(g.items) == 0 {
		return false
	}
	for _, it := range g.items {
		if it.State != planner.StateDone && it.State != planner.StateFailed {
			return false
		}
	}
	return true
}

// anyFailed reports whether any line in the group failed.
func (g *Group) anyFailed() bool {
	for _, it := range g.items {
		if it.State == planner.StateFailed {
			return true
		}
	}
	return false
}

// started reports whether any line in the group has left StatePending.
func (g *Group) started() bool {
	for _, it := range g.items {
		if it.State != planner.StatePending {
			return true
		}
	}
	return false
}

// collapsed reports whether the group renders as a single summary line.
//
// A group collapses at BOTH ends of its life: before anything in it has started,
// and once everything has finished. The spec's own mock shows the first case
// (". Filesets  3 pending") and it is what keeps the frame inside the terminal —
// with only the finished case, every group of every context is fully expanded at
// seed time, which on a real manifest is ~92 lines against a 24-line terminal, and
// Bubble Tea keeps only the LAST height lines.
//
// A group holding a failure never collapses at either end, so a failure cannot
// hide behind a summary.
func (g *Group) collapsed() bool {
	if g.anyFailed() {
		return false
	}
	return !g.started() || g.terminal()
}

type viewState int

const (
	stateRunning viewState = iota
	stateFinal
)

// Model is the apply view. It is a Bubble Tea model rendered inline.
type Model struct {
	width    int
	height   int
	groups   []*Group
	index    map[planner.ResourceRef]*Item
	frame    int
	state    viewState
	logPath  string
	started  time.Time
	cancelCh chan struct{}
	now      func() time.Time
}

// New creates an empty model. now is injected so tests are deterministic; pass
// time.Now in production.
func New(now func() time.Time) Model {
	if now == nil {
		now = time.Now
	}
	return Model{
		width: 80,
		index: map[planner.ResourceRef]*Item{},
		now:   now,
	}
}

// WithCancel attaches the channel signalled when the user presses Ctrl+C.
func (m Model) WithCancel(ch chan struct{}) Model {
	m.cancelCh = ch
	return m
}

// Counts returns how many lines are terminal, and how many exist in total.
// Counts returns how many changes are terminal and how many there are, matching
// what the plan footer the user just approved reported.
//
// A ResourceStack line is NOT counted (see countsTowardTotal in cascade.go):
// it is the same work as its services seen at a coarser grain — apply drives
// one compose call per stack — so counting both reported 15 where the plan
// said 12. The plan counts services and never the stack (see
// ResourcePlan.CountActions), so the view follows it.
func (m Model) Counts() (done, total int) {
	for _, g := range m.groups {
		for _, it := range g.items {
			if !countsTowardTotal(it.Ref) {
				continue
			}
			total++
			if it.State == planner.StateDone || it.State == planner.StateFailed {
				done++
			}
		}
	}
	return done, total
}

// Failures returns every failed line, in render order.
func (m Model) Failures() []*Item {
	var out []*Item
	for _, g := range m.groups {
		for _, it := range g.items {
			if it.State == planner.StateFailed {
				out = append(out, it)
			}
		}
	}
	return out
}

// Outcomes splits the counted changes into the three ways a run can leave one,
// so the numbers a reader sees actually reconcile: applied + interrupted +
// notApplied == total from Counts.
//
// Failures are NOT one of these buckets. A failed line is a stack, which Counts
// deliberately excludes as the coarse view of its own services — so a failure is
// the CAUSE of services sitting in notApplied, not a fourth category competing
// with them. Reporting "2/5 applied · 1 failed" on its own invited exactly the
// arithmetic that does not work (a reader tried it and got 4 of 5).
func (m Model) Outcomes() (applied, interrupted, notApplied int) {
	for _, g := range m.groups {
		for _, it := range g.items {
			if !countsTowardTotal(it.Ref) {
				continue
			}
			switch it.State {
			case planner.StateDone, planner.StateFailed:
				applied++
			case planner.StateRunning:
				interrupted++
			default:
				notApplied++
			}
		}
	}
	return applied, interrupted, notApplied
}

func (m Model) itemFor(ref planner.ResourceRef) *Item { return m.index[ref] }

func (m Model) groupFor(ref planner.ResourceRef) *Group {
	title := planner.SectionTitle(ref.Type)
	for _, g := range m.groups {
		if g.Context == ref.Context && g.Title == title {
			return g
		}
	}
	return nil
}

// ensureGroup returns the group for ref, creating it if absent. Groups keep
// creation order, which for a seeded run is the plan's order.
func (m *Model) ensureGroup(ref planner.ResourceRef) *Group {
	if g := m.groupFor(ref); g != nil {
		return g
	}
	g := &Group{Context: ref.Context, Title: planner.SectionTitle(ref.Type)}
	m.groups = append(m.groups, g)
	return g
}

// ensureItem returns the line for ref, appending a discovered one if apply
// reported work the plan did not contain. Dropping the event instead would make
// the view understate the run's real scope.
func (m *Model) ensureItem(ref planner.ResourceRef, discovered bool) *Item {
	if it, ok := m.index[ref]; ok {
		return it
	}
	it := &Item{Ref: ref, State: planner.StatePending, Discovered: discovered}
	g := m.ensureGroup(ref)
	g.items = append(g.items, it)
	m.index[ref] = it
	return it
}

func (m Model) Init() tea.Cmd { return tickCmd() }

func tickCmd() tea.Cmd {
	return tea.Tick(tickDelay, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC && m.cancelCh != nil {
			select {
			case m.cancelCh <- struct{}{}:
			default:
			}
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case SeedMsg:
		if m.started.IsZero() {
			m.started = m.now()
		}
		for _, ref := range msg.Items {
			m.ensureItem(ref, false)
		}
		return m, nil

	case StartMsg:
		if m.started.IsZero() {
			m.started = m.now()
		}
		it := m.ensureItem(msg.Ref, true)
		it.State = planner.StateRunning
		it.Verb = msg.Verb
		it.Detail = ""
		it.Percent = 0
		it.HasPercent = false
		it.started = m.now()
		return m, nil

	case DetailMsg:
		it := m.ensureItem(msg.Ref, true)
		it.Detail = msg.Text
		return m, nil

	case ProgressMsg:
		// itemFor, not ensureItem: a percentage must never create a line, and
		// must never touch one that is not running.
		it := m.itemFor(msg.Ref)
		if it == nil || it.State != planner.StateRunning {
			return m, nil
		}
		it.Percent, it.HasPercent = msg.Percent, true
		return m, nil

	case FinishMsg:
		it := m.ensureItem(msg.Ref, true)
		if msg.Result == "" && it.Discovered {
			// apply reported this ref only via Start (e.g. a fileset whose tree
			// hash still matched at apply time, so there was nothing to sync)
			// and it was never in the plan's own seed. An empty Finish result is
			// the "found nothing to do" signal: void the line entirely rather
			// than count it, so the header total does not grow mid-run for work
			// that never existed. See dropItem.
			m.dropItem(it)
			return m, nil
		}
		m.markDone(it, msg.Result)
		// A stack is applied by a single compose call, so its seeded services
		// resolve with it. See the Granularity section of the spec.
		if msg.Ref.Type == planner.ResourceStack {
			if g := m.groupFor(msg.Ref); g != nil {
				for _, child := range g.items {
					if resolvesWithStack(msg.Ref, child.Ref, child.State) {
						m.markDone(child, msg.Result)
					}
				}
			}
		}
		return m, nil

	case FailMsg:
		it := m.ensureItem(msg.Ref, true)
		it.State = planner.StateFailed
		it.Err = msg.Err
		it.elapsed = m.elapsedFor(it)
		// Children are deliberately left alone: a stack failing does not mean
		// every service in it failed.
		return m, nil

	case tickMsg:
		if m.state == stateRunning {
			m.frame++
			return m, tickCmd()
		}
		return m, nil

	case DoneMsg:
		m.logPath = msg.LogPath
		m.state = stateFinal
		return m, tea.Quit
	}

	return m, nil
}

// dropItem removes it from the model as though it had never been reported:
// used only for a discovered item (one Start created rather than Seed) whose
// Finish carried an empty result, meaning apply found nothing to do for it.
func (m *Model) dropItem(it *Item) {
	delete(m.index, it.Ref)
	g := m.groupFor(it.Ref)
	if g == nil {
		return
	}
	for i, x := range g.items {
		if x == it {
			g.items = append(g.items[:i], g.items[i+1:]...)
			break
		}
	} // Drop the group with its last item. A voided line is usually the ONLY line
	// in its group — an unchanged fileset is started, then voided — and leaving
	// the husk behind rendered a phantom "Filesets  0 pending" under a repeated
	// context header, because the empty group had been appended at discovery
	// time, after every seeded group.
	if len(g.items) == 0 {
		for i, x := range m.groups {
			if x == g {
				m.groups = append(m.groups[:i], m.groups[i+1:]...)
				break
			}
		}
	}
}

func (m Model) markDone(it *Item, result string) {
	it.State = planner.StateDone
	it.Result = result
	it.elapsed = m.elapsedFor(it)
}

func (m Model) elapsedFor(it *Item) time.Duration {
	if it.started.IsZero() {
		return 0
	}
	return m.now().Sub(it.started)
}
