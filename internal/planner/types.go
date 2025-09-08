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
	filesetLines := []ui.DiffLine{}
	other := []ui.DiffLine{}

	for _, l := range pln.Lines {
		m := l.Message
		switch {
		case strings.HasPrefix(m, "volume "):
			vols = append(vols, l)
		case strings.HasPrefix(m, "network "):
			nets = append(nets, l)
		case strings.HasPrefix(m, "fileset "):
			filesetLines = append(filesetLines, l)
		case strings.HasPrefix(m, "service ") || strings.HasPrefix(m, "application ") || strings.HasPrefix(m, "container "):
			apps = append(apps, l)
		default:
			other = append(other, l)
		}
	}

	// Build sections with nested filesets
	sections := []ui.NestedSection{
		{Title: "Volumes", Items: vols},
		{Title: "Networks", Items: nets},
		{Title: "Applications", Items: apps},
		buildFilesetSection(filesetLines),
	}

	if len(other) > 0 {
		sections = append(sections, ui.NestedSection{Title: "Other", Items: other})
	}

	return ui.RenderNestedSections(sections)
}

// buildFilesetSection groups fileset lines by fileset name and creates nested structure.
func buildFilesetSection(filesetLines []ui.DiffLine) ui.NestedSection {
	if len(filesetLines) == 0 {
		return ui.NestedSection{Title: "Filesets"}
	}

	// Group fileset lines by fileset name
	filesetGroups := make(map[string][]ui.DiffLine)

	for _, line := range filesetLines {
		// Extract fileset name from message like "fileset myfiles: create file.txt"
		msg := line.Message
		if strings.HasPrefix(msg, "fileset ") {
			parts := strings.SplitN(msg[8:], ": ", 2) // Remove "fileset " prefix
			if len(parts) == 2 {
				filesetName := parts[0]
				action := parts[1]

				// Create a new line with just the action (without fileset name)
				actionLine := ui.DiffLine{
					Type:    line.Type,
					Message: action,
				}

				filesetGroups[filesetName] = append(filesetGroups[filesetName], actionLine)
			}
		}
	}

	// Convert to sorted nested sections
	var nestedSections []ui.NestedSection
	filesetNames := make([]string, 0, len(filesetGroups))
	for name := range filesetGroups {
		filesetNames = append(filesetNames, name)
	}
	sort.Strings(filesetNames)

	for _, name := range filesetNames {
		nestedSections = append(nestedSections, ui.NestedSection{
			Title: name,
			Items: filesetGroups[name],
		})
	}

	return ui.NestedSection{
		Title:    "Filesets",
		Sections: nestedSections,
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
