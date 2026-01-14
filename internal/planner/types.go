package planner

import (
	"sort"

	"github.com/gcstr/dockform/internal/filesets"
)

// Plan represents a structured plan with resources organized by daemon and type.
type Plan struct {
	// Resources organized by daemon
	ByDaemon map[string]*DaemonPlan

	// Aggregated resource plan (for display - combines all daemons)
	Resources *ResourcePlan

	// Multi-daemon execution context
	ExecutionContext *MultiDaemonExecutionContext
}

// DaemonPlan represents the plan for a single daemon.
type DaemonPlan struct {
	DaemonName string
	Context    string
	Identifier string
	Resources  *ResourcePlan
}

// MultiDaemonExecutionContext contains pre-computed data for all daemons.
type MultiDaemonExecutionContext struct {
	// Per-daemon execution contexts
	ByDaemon map[string]*DaemonExecutionContext
}

// DaemonExecutionContext contains pre-computed data needed to execute the plan
// for a single daemon. This allows Apply to reuse state detection results from
// BuildPlan, avoiding duplicate Docker API calls, SOPS decryption, and compose
// config parsing.
type DaemonExecutionContext struct {
	DaemonName string
	Context    string // Docker context
	Identifier string

	// Per-stack execution data (keys are stack names without daemon prefix)
	Stacks map[string]*StackExecutionData
	// Per-fileset execution data (indexes and diffs computed during plan)
	Filesets map[string]*FilesetExecutionData
	// Snapshot of existing volumes (used for fileset sync validation and progress estimation)
	ExistingVolumes map[string]struct{}
	// Snapshot of existing networks (used for progress estimation)
	ExistingNetworks map[string]struct{}
}

// StackExecutionData contains pre-computed data for applying a stack
type StackExecutionData struct {
	// Full service state detection results
	Services []ServiceInfo
	// Pre-built inline environment (with decrypted secrets)
	InlineEnv []string
	// Whether this stack needs compose up
	NeedsApply bool
}

// FilesetExecutionData contains pre-computed fileset indexes and diffs to avoid redundant
// filesystem walks, volume reads, and SHA256 computations during apply phase.
type FilesetExecutionData struct {
	// Local filesystem index (files + SHA256 hashes)
	LocalIndex filesets.Index
	// Remote volume index (if volume exists)
	RemoteIndex filesets.Index
	// Diff between local and remote
	Diff filesets.Diff
}

func (pln *Plan) String() string {
	if pln.Resources == nil {
		return "[no plan]"
	}
	return RenderResourcePlan(pln.Resources)
}

// GetDaemonContext returns the execution context for a specific daemon.
func (pln *Plan) GetDaemonContext(daemonName string) *DaemonExecutionContext {
	if pln.ExecutionContext == nil || pln.ExecutionContext.ByDaemon == nil {
		return nil
	}
	return pln.ExecutionContext.ByDaemon[daemonName]
}

// GetDaemonNames returns sorted list of daemon names in the plan.
func (pln *Plan) GetDaemonNames() []string {
	if pln.ByDaemon == nil {
		return nil
	}
	return sortedKeys(pln.ByDaemon)
}

// CountChanges returns the total number of changes across all daemons.
func (pln *Plan) CountChanges() (add, update, remove int) {
	if pln.Resources == nil {
		return 0, 0, 0
	}
	return pln.Resources.CountActions()
}

// IsEmpty returns true if there are no changes in the plan.
func (pln *Plan) IsEmpty() bool {
	add, update, remove := pln.CountChanges()
	return add == 0 && update == 0 && remove == 0
}

// NewMultiDaemonExecutionContext creates a new empty multi-daemon execution context.
func NewMultiDaemonExecutionContext() *MultiDaemonExecutionContext {
	return &MultiDaemonExecutionContext{
		ByDaemon: make(map[string]*DaemonExecutionContext),
	}
}

// NewDaemonExecutionContext creates a new empty daemon execution context.
func NewDaemonExecutionContext(daemonName, context, identifier string) *DaemonExecutionContext {
	return &DaemonExecutionContext{
		DaemonName:       daemonName,
		Context:          context,
		Identifier:       identifier,
		Stacks:           make(map[string]*StackExecutionData),
		Filesets:         make(map[string]*FilesetExecutionData),
		ExistingVolumes:  make(map[string]struct{}),
		ExistingNetworks: make(map[string]struct{}),
	}
}

// sortedKeys returns sorted keys of a map[string]T
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Legacy support: ExecutionContext for single-daemon backward compatibility
// This is used during the transition period and maps to the first daemon's context.
type ExecutionContext = DaemonExecutionContext
