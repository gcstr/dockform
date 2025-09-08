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
		case strings.HasPrefix(m, "service ") || strings.HasPrefix(m, "application "):
			apps = append(apps, l)
		case strings.HasPrefix(m, "container "):
			other = append(other, l)
		default:
			other = append(other, l)
		}
	}

	// Build sections with nested applications and filesets
	sections := []ui.NestedSection{
		{Title: "Volumes", Items: vols},
		{Title: "Networks", Items: nets},
		buildApplicationSection(apps),
		buildFilesetSection(filesetLines),
	}

	if len(other) > 0 {
		sections = append(sections, ui.NestedSection{Title: "Other", Items: other})
	}

	// Calculate summary counts
	var createCount, changeCount, destroyCount int
	for _, line := range pln.Lines {
		switch line.Type {
		case ui.Add:
			createCount++
		case ui.Change:
			changeCount++
		case ui.Remove:
			destroyCount++
		}
	}

	// Render sections and add summary line
	result := ui.RenderNestedSections(sections)

	// Add summary line with single line spacing if there's content
	if len(pln.Lines) > 0 {
		if result != "" {
			result += "\n"
		}
		summary := ui.FormatPlanSummary(createCount, changeCount, destroyCount)
		result += summary
	}

	return result
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

// buildApplicationSection groups application lines by application name and creates nested structure.
func buildApplicationSection(applicationLines []ui.DiffLine) ui.NestedSection {
	if len(applicationLines) == 0 {
		return ui.NestedSection{Title: "Applications"}
	}

	// Group application lines by application name
	appGroups := make(map[string][]ui.DiffLine)

	for _, line := range applicationLines {
		// Extract application name from messages like:
		// "service linkwarden/linkwarden will be started"
		// "application myapp planned (services diff TBD)"
		msg := line.Message

		if strings.HasPrefix(msg, "service ") {
			// Parse "service appName/serviceName action" format
			parts := strings.SplitN(msg[8:], "/", 2) // Remove "service " prefix
			if len(parts) == 2 {
				appName := parts[0]
				// Extract service name and action from "serviceName action"
				serviceAndAction := parts[1]
				spaceIndex := strings.Index(serviceAndAction, " ")
				if spaceIndex > 0 {
					serviceName := serviceAndAction[:spaceIndex]
					action := serviceAndAction[spaceIndex+1:]

					// Create a new line with just the service and action
					actionLine := ui.DiffLine{
						Type:    line.Type,
						Message: serviceName + " " + action,
					}

					appGroups[appName] = append(appGroups[appName], actionLine)
				}
			}
		} else if strings.HasPrefix(msg, "application ") {
			// Parse "application appName planned (services diff TBD)" format
			parts := strings.SplitN(msg[12:], " ", 2) // Remove "application " prefix
			if len(parts) >= 1 {
				appName := parts[0]
				action := "planned (services diff TBD)"
				if len(parts) > 1 {
					action = parts[1]
				}

				actionLine := ui.DiffLine{
					Type:    line.Type,
					Message: action,
				}

				appGroups[appName] = append(appGroups[appName], actionLine)
			}
		}
	}

	// Convert to sorted nested sections
	var nestedSections []ui.NestedSection
	appNames := make([]string, 0, len(appGroups))
	for name := range appGroups {
		appNames = append(appNames, name)
	}
	sort.Strings(appNames)

	for _, name := range appNames {
		nestedSections = append(nestedSections, ui.NestedSection{
			Title: name,
			Items: appGroups[name],
		})
	}

	return ui.NestedSection{
		Title:    "Applications",
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
