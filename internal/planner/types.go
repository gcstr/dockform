package planner

import (
	"sort"
	"strings"

	"github.com/gcstr/dockform/internal/ui"
)

// Plan represents a set of diff lines to show to the user.
type Plan struct {
	Lines []ui.DiffLine
}

func (pln *Plan) String() string {
	// Group lines by high-level category based on message prefix
	vols := []ui.DiffLine{}
	nets := []ui.DiffLine{}
	apps := []ui.DiffLine{}
	files := []ui.DiffLine{}
	other := []ui.DiffLine{}
	for _, l := range pln.Lines {
		m := l.Message
		switch {
		case strings.HasPrefix(m, "volume "):
			vols = append(vols, l)
		case strings.HasPrefix(m, "network "):
			nets = append(nets, l)
		case strings.HasPrefix(m, "fileset "):
			files = append(files, l)
		case strings.HasPrefix(m, "service ") || strings.HasPrefix(m, "application ") || strings.HasPrefix(m, "container "):
			apps = append(apps, l)
		default:
			other = append(other, l)
		}
	}
	sections := []ui.Section{
		{Title: "Volumes", Items: vols},
		{Title: "Networks", Items: nets},
		{Title: "Applications", Items: apps},
		{Title: "Filesets", Items: files},
	}
	if len(other) > 0 {
		sections = append(sections, ui.Section{Title: "Other", Items: other})
	}
	return ui.RenderSectionedList(sections)
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
