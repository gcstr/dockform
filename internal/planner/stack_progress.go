package planner

import (
	"sort"
	"strings"

	"github.com/gcstr/dockform/internal/dockercli"
)

// stackProgress turns one stack's compose progress events into per-service
// status lines.
//
// It is advisory by construction: it never returns an error, it only ever
// touches the lines the plan seeded — the stack's changed services, plus detail
// on the stack line itself — and it never sets the stack line's final state.
// apply.go does that from ComposeUpWithProgress's return value.
type stackProgress struct {
	reporter ProgressReporter
	context  string
	stack    string
	changed  map[string]bool
	resolver containerResolver

	lines   map[string]*serviceLine
	pulls   map[string]*pullProgress
	waiting map[string]bool

	// first identifies the event that opened the run. exec.go retries an SSH
	// failure and compose replays its whole stream, so seeing that exact event
	// again is the one reliable marker that a new attempt has started.
	first     eventID
	firstSeen bool
}

// eventID is an event's identity for replay detection. Layer events are
// excluded by noteAttempt: a layer can repeat an identical event within one
// attempt (measured in pull_shared_base_layer.jsonl), and the first event of a
// pull is always the image's own "Pulling" anyway.
type eventID struct {
	kind   dockercli.EventKind
	name   string
	status string
	text   string
}

type serviceLine struct {
	state  ResourceState
	result string // the last action verb seen: "created" or "recreated"
}

func newStackProgress(reporter ProgressReporter, contextName, stackName string, changed map[string]bool, resolver containerResolver) *stackProgress {
	return &stackProgress{
		reporter: orNop(reporter),
		context:  contextName,
		stack:    stackName,
		changed:  changed,
		resolver: resolver,
		lines:    map[string]*serviceLine{},
		pulls:    map[string]*pullProgress{},
		waiting:  map[string]bool{},
	}
}

// containerVerbs maps compose's Working texts to the verb a line shows. Texts
// not listed here — including "Waiting", handled separately — are not phases
// of the service's own work.
var containerVerbs = map[string]string{
	"Creating": "creating",
	"Recreate": "recreating",
	"Starting": "starting",
}

// OnEvent handles one event. It is the callback handed to ComposeUpWithProgress.
func (s *stackProgress) OnEvent(ev dockercli.ComposeEvent) {
	s.noteAttempt(ev)
	switch ev.Kind {
	case dockercli.EventContainer:
		s.onContainer(ev)
	case dockercli.EventImage:
		s.onImage(ev)
	case dockercli.EventLayer:
		s.onLayer(ev)
	}
}

// noteAttempt drops the state a new attempt invalidates. Only waits are
// transient this way: a wait the dropped attempt never resolved is never
// cleared, because attempt 2 may not need to wait on that dependency again —
// it would sit on the stack line for the whole rest of the run. Everything
// else a replay touches is either idempotent or folded with max.
func (s *stackProgress) noteAttempt(ev dockercli.ComposeEvent) {
	if ev.Kind == dockercli.EventLayer {
		return
	}
	id := eventID{ev.Kind, ev.Name, ev.Status, ev.Text}
	if !s.firstSeen {
		s.first, s.firstSeen = id, true
		return
	}
	if id != s.first || len(s.waiting) == 0 {
		return
	}
	clear(s.waiting)
	s.renderWaits()
}

func (s *stackProgress) onContainer(ev dockercli.ComposeEvent) {
	svc, resolved := s.resolver.service(ev.Name)

	// A healthcheck wait is reported on the dependency, which is usually not a
	// changed service and has no line — and which emits "Waiting" after it has
	// already "Started". Every wait therefore goes to the stack line.
	if ev.Text == "Waiting" || ev.Text == "Healthy" {
		name := ev.Name
		if resolved {
			name = svc
		}
		if ev.Text == "Waiting" {
			s.waiting[name] = true
		} else {
			delete(s.waiting, name)
		}
		s.renderWaits()
		return
	}

	if !resolved || !s.changed[svc] {
		return
	}
	line := s.line(svc)
	if line.state == StateDone {
		return
	}
	switch ev.Status {
	case "Working":
		if verb, ok := containerVerbs[ev.Text]; ok {
			s.advance(svc, line, verb)
		}
	case "Done":
		switch ev.Text {
		case "Created":
			line.result = "created"
		case "Recreated":
			line.result = "recreated"
		case "Started":
			result := line.result
			if result == "" {
				result = "started"
			}
			line.state = StateDone
			s.reporter.Finish(s.ref(svc), result)
		}
	}
}

func (s *stackProgress) onImage(ev dockercli.ComposeEvent) {
	if ev.Status != "Working" || ev.Text != "Pulling" {
		return
	}
	// A new pull of this image starts here. On the first attempt this is a
	// no-op; on an SSH retry it discards the previous attempt's byte maxima and
	// high-water mark, which would otherwise freeze the line at the percentage
	// the dropped attempt reached until the re-download passed it.
	delete(s.pulls, ev.Name)
	for _, svc := range s.resolver.servicesForImage(ev.Name) {
		if !s.changed[svc] {
			continue
		}
		if line := s.line(svc); line.state != StateDone {
			s.advance(svc, line, "pulling")
		}
	}
}

func (s *stackProgress) onLayer(ev dockercli.ComposeEvent) {
	p, ok := s.pulls[ev.Image]
	if !ok {
		p = newPullProgress()
		s.pulls[ev.Image] = p
	}
	pct, report := p.observe(ev)
	if !report {
		return
	}
	for _, svc := range s.resolver.servicesForImage(ev.Image) {
		if !s.changed[svc] {
			continue
		}
		if line := s.line(svc); line.state == StateRunning {
			s.reporter.Progress(s.ref(svc), pct)
		}
	}
}

// advance moves a line into its next phase. The first phase uses Start; later
// phases use Detail, because Start resets a line's start time and would
// restart its elapsed clock at every phase.
func (s *stackProgress) advance(svc string, line *serviceLine, verb string) {
	if line.state == StatePending {
		line.state = StateRunning
		s.reporter.Start(s.ref(svc), verb)
		return
	}
	s.reporter.Detail(s.ref(svc), verb+"…")
}

// renderWaits shows every current wait on the stack line, sorted, or clears the
// detail when there are none — the stack line then falls back to its own verb.
func (s *stackProgress) renderWaits() {
	if len(s.waiting) == 0 {
		s.reporter.Detail(s.stackRef(), "")
		return
	}
	names := make([]string, 0, len(s.waiting))
	for name := range s.waiting {
		names = append(names, name)
	}
	sort.Strings(names)
	s.reporter.Detail(s.stackRef(), "waiting: "+strings.Join(names, ", ")+" healthy")
}

func (s *stackProgress) line(svc string) *serviceLine {
	l, ok := s.lines[svc]
	if !ok {
		l = &serviceLine{state: StatePending}
		s.lines[svc] = l
	}
	return l
}

func (s *stackProgress) ref(svc string) ResourceRef {
	return ResourceRef{Context: s.context, Type: ResourceService, Name: svc, Parent: s.stack}
}

func (s *stackProgress) stackRef() ResourceRef {
	return ResourceRef{Context: s.context, Type: ResourceStack, Name: s.stack}
}
