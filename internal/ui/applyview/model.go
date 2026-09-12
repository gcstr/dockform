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

// collapsed reports whether the group renders as a single summary line. A group
// holding a failure never collapses, so a failure cannot hide behind a summary.
func (g *Group) collapsed() bool { return g.terminal() && !g.anyFailed() }

type viewState int

const (
	stateRunning viewState = iota
	stateFinal
)

// Model is the apply view. It is a Bubble Tea model rendered inline.
type Model struct {
	width    int
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
func (m Model) Counts() (done, total int) {
	for _, g := range m.groups {
		for _, it := range g.items {
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

func (m Model) itemFor(ref planner.ResourceRef) *Item { return m.index[ref] }

// groupTitle maps a resource type to its section heading. Stacks and the
// services nested under them share one section, as in the plan renderer.
func groupTitle(t planner.ResourceType) string {
	switch t {
	case planner.ResourceVolume:
		return "Volumes"
	case planner.ResourceNetwork:
		return "Networks"
	case planner.ResourceStack, planner.ResourceService:
		return "Stacks"
	case planner.ResourceFileset, planner.ResourceFile:
		return "Filesets"
	case planner.ResourceContainer:
		return "Containers"
	default:
		return "Other"
	}
}

func (m Model) groupFor(ref planner.ResourceRef) *Group {
	title := groupTitle(ref.Type)
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
	g := &Group{Context: ref.Context, Title: groupTitle(ref.Type)}
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
		it.started = m.now()
		return m, nil

	case DetailMsg:
		it := m.ensureItem(msg.Ref, true)
		it.Detail = msg.Text
		return m, nil

	case FinishMsg:
		it := m.ensureItem(msg.Ref, true)
		m.markDone(it, msg.Result)
		// A stack is applied by a single compose call, so its seeded services
		// resolve with it. See the Granularity section of the spec.
		if msg.Ref.Type == planner.ResourceStack {
			if g := m.groupFor(msg.Ref); g != nil {
				for _, child := range g.items {
					if child.Ref.Type == planner.ResourceService &&
						child.Ref.Parent == msg.Ref.Name &&
						child.State != planner.StateFailed {
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

// View is a placeholder so Model satisfies tea.Model; the real renderer lands in
// the next task, which replaces this wholesale.
func (m Model) View() string { return "" }
