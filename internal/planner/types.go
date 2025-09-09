package planner

import (
	"sort"
)

// Plan represents a structured plan with resources organized by type
type Plan struct {
	Resources *ResourcePlan
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

