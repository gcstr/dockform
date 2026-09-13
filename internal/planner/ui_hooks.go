package planner

// ResourceRef identifies one line in the apply view. It is the identity the
// renderer keys on, so every event about the same resource must carry an equal
// value — all fields participate in comparison.
type ResourceRef struct {
	Context string // docker context the resource belongs to
	Type    ResourceType
	Name    string
	Parent  string // stack name for a service; fileset name for a file
}

// ResourceState is the lifecycle state of a single line.
type ResourceState int

const (
	StatePending ResourceState = iota
	StateRunning
	StateDone
	StateFailed
)

// ProgressReporter receives resource-keyed lifecycle events while apply runs.
// Implementations must be safe for concurrent use: apply processes contexts in
// parallel.
type ProgressReporter interface {
	// Seed declares every line up front, all in StatePending.
	Seed(items []ResourceRef)
	// Start moves a line to StateRunning. verb is a present participle with no
	// resource name in it: "creating", "starting", "syncing".
	Start(ref ResourceRef, verb string)
	// Detail replaces a running line's sub-status, e.g. "8/23 files".
	Detail(ref ResourceRef, text string)
	// Finish moves a line to StateDone. result is past tense: "created". An
	// empty result is a distinct "found nothing to do" signal for a ref that
	// was only ever reported via Start, never seeded: both renderers treat it
	// as void — dropping the line entirely rather than counting it — so
	// discovering that a resource needed no work does not inflate the run's
	// totals. Never pass "" for a ref that Seed already declared.
	Finish(ref ResourceRef, result string)
	// Fail moves a line to StateFailed and records the cause.
	Fail(ref ResourceRef, err error)
}

// nopReporter satisfies ProgressReporter and discards everything.
type nopReporter struct{}

func (nopReporter) Seed([]ResourceRef)         {}
func (nopReporter) Start(ResourceRef, string)  {}
func (nopReporter) Detail(ResourceRef, string) {}
func (nopReporter) Finish(ResourceRef, string) {}
func (nopReporter) Fail(ResourceRef, error)    {}

// orNop returns r, or a no-op reporter when r is nil. Holders store the result,
// so no call site needs a nil check and no nil interface can be dereferenced.
func orNop(r ProgressReporter) ProgressReporter {
	if r == nil {
		return nopReporter{}
	}
	return r
}
