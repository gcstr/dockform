package planner

import (
	"sort"

	"github.com/gcstr/dockform/internal/filesets"
)

// Plan represents a structured plan with resources organized by type
type Plan struct {
	Resources        *ResourcePlan
	ExecutionContext *ExecutionContext
}

// ExecutionContext contains pre-computed data needed to execute the plan efficiently.
// This allows Apply to reuse state detection results from BuildPlan, avoiding duplicate
// Docker API calls, SOPS decryption, and compose config parsing.
type ExecutionContext struct {
	// Per-stack execution data
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

// sortedKeys returns sorted keys of a map[string]T
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
