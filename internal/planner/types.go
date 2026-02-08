package planner

import (
	"sort"

	"github.com/gcstr/dockform/internal/filesets"
)

// Plan represents a structured plan with resources organized by context and type.
type Plan struct {
	// Resources organized by context
	ByContext map[string]*ContextPlan

	// Aggregated resource plan (for display - combines all contexts)
	Resources *ResourcePlan

	// Multi-context execution context
	ExecutionContext *MultiContextExecutionContext
}

// ContextPlan represents the plan for a single context.
type ContextPlan struct {
	ContextName string
	Identifier  string
	Resources   *ResourcePlan
}

// MultiContextExecutionContext contains pre-computed data for all contexts.
type MultiContextExecutionContext struct {
	// Per-context execution contexts
	ByContext map[string]*ContextExecutionContext
}

// ContextExecutionContext contains pre-computed data needed to execute the plan
// for a single context. This allows Apply to reuse state detection results from
// BuildPlan, avoiding duplicate Docker API calls, SOPS decryption, and compose
// config parsing.
type ContextExecutionContext struct {
	ContextName string
	Identifier  string

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

// GetContextExecutionContext returns the execution context for a specific context.
func (pln *Plan) GetContextExecutionContext(contextName string) *ContextExecutionContext {
	if pln.ExecutionContext == nil || pln.ExecutionContext.ByContext == nil {
		return nil
	}
	return pln.ExecutionContext.ByContext[contextName]
}

// GetContextNames returns sorted list of context names in the plan.
func (pln *Plan) GetContextNames() []string {
	if pln.ByContext == nil {
		return nil
	}
	return sortedKeys(pln.ByContext)
}

// CountChanges returns the total number of changes across all contexts.
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

// NewMultiContextExecutionContext creates a new empty multi-context execution context.
func NewMultiContextExecutionContext() *MultiContextExecutionContext {
	return &MultiContextExecutionContext{
		ByContext: make(map[string]*ContextExecutionContext),
	}
}

// NewContextExecutionContext creates a new empty context execution context.
func NewContextExecutionContext(contextName, identifier string) *ContextExecutionContext {
	return &ContextExecutionContext{
		ContextName:      contextName,
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

// Legacy support: ExecutionContext for single-context backward compatibility
// This is used during the transition period and maps to the first context's execution context.
type ExecutionContext = ContextExecutionContext
