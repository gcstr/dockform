package imagescmd

import (
	"sort"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/images"
)

// filterInputsByServices narrows the Services map of each CheckInput to only
// those whose names appear in serviceNames. If serviceNames is empty, inputs
// is returned unchanged (callers want everything in scope).
//
// An empty result triggers an apperr.InvalidInput error that lists the
// services available across the inputs — typos like "backp" should fail loud
// rather than silently no-op.
func filterInputsByServices(inputs []images.CheckInput, serviceNames []string) ([]images.CheckInput, error) {
	if len(serviceNames) == 0 {
		return inputs, nil
	}

	wanted := make(map[string]struct{}, len(serviceNames))
	for _, n := range serviceNames {
		wanted[n] = struct{}{}
	}

	filtered := make([]images.CheckInput, 0, len(inputs))
	matched := 0
	for _, in := range inputs {
		kept := make(map[string]images.ServiceSpec)
		for svc, spec := range in.Services {
			if _, ok := wanted[svc]; ok {
				kept[svc] = spec
				matched++
			}
		}
		if len(kept) == 0 {
			continue
		}
		filtered = append(filtered, images.CheckInput{
			StackKey: in.StackKey,
			Services: kept,
		})
	}

	if matched == 0 {
		return nil, apperr.New(
			"imagescmd.filterInputsByServices",
			apperr.InvalidInput,
			"no services matched %s\n\navailable services:\n%s",
			formatServiceNames(serviceNames),
			formatAvailableServices(inputs),
		)
	}

	return filtered, nil
}

// formatServiceNames renders a list of requested service names as a
// human-readable, quoted, comma-separated list: `"app", "web"`.
func formatServiceNames(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = `"` + n + `"`
	}
	return strings.Join(quoted, ", ")
}

// formatAvailableServices lists every service across every input, grouped by
// stack, sorted for deterministic output.
func formatAvailableServices(inputs []images.CheckInput) string {
	if len(inputs) == 0 {
		return "  (none — scope is empty)"
	}

	// Sort stacks first for stable output.
	sorted := make([]images.CheckInput, len(inputs))
	copy(sorted, inputs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StackKey < sorted[j].StackKey
	})

	var b strings.Builder
	for i, in := range sorted {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("  ")
		b.WriteString(in.StackKey)
		b.WriteString(": ")

		names := make([]string, 0, len(in.Services))
		for svc := range in.Services {
			names = append(names, svc)
		}
		sort.Strings(names)
		b.WriteString(strings.Join(names, ", "))
	}
	return b.String()
}
