package planner

import (
	"fmt"
	"sort"

	"github.com/gcstr/dockform/internal/ui"
)

// Action represents a standardized action that can be taken on a resource
type Action string

const (
	ActionCreate    Action = "create"
	ActionUpdate    Action = "update"
	ActionDelete    Action = "delete"
	ActionReconcile Action = "reconcile"
	ActionNoop      Action = "no-op"
)

// ResourceType represents the type of infrastructure resource
type ResourceType string

const (
	ResourceVolume    ResourceType = "volume"
	ResourceNetwork   ResourceType = "network"
	ResourceService   ResourceType = "service"
	ResourceContainer ResourceType = "container"
	ResourceFileset   ResourceType = "fileset"
	ResourceFile      ResourceType = "file" // Individual file in a fileset
)

// Resource represents a single infrastructure resource with its planned action
type Resource struct {
	Type       ResourceType
	Name       string        // e.g., "traefik_config" for volumes, "linkwarden/postgres" for services
	Action     Action        // The action to be taken
	Details    string        // Optional details about the action
	Parent     string        // For nested resources (e.g., fileset name for files)
	ChangeType ui.ChangeType // Maps to UI change type for rendering
}

// ResourcePlan represents a structured plan with resources organized by type
type ResourcePlan struct {
	Volumes    []Resource
	Networks   []Resource
	Stacks     map[string][]Resource // Stack name -> services
	Filesets   map[string][]Resource // Fileset name -> file changes
	Containers []Resource            // Orphaned containers to remove
}

// NewResource creates a new resource with the appropriate change type
func NewResource(resType ResourceType, name string, action Action, details string) Resource {
	return Resource{
		Type:       resType,
		Name:       name,
		Action:     action,
		Details:    details,
		ChangeType: actionToChangeType(action),
	}
}

// NewNestedResource creates a resource that belongs to a parent (e.g., service in app, file in fileset)
func NewNestedResource(resType ResourceType, name string, parent string, action Action, details string) Resource {
	res := NewResource(resType, name, action, details)
	res.Parent = parent
	return res
}

// actionToChangeType maps actions to UI change types
func actionToChangeType(action Action) ui.ChangeType {
	switch action {
	case ActionCreate:
		return ui.Add
	case ActionUpdate:
		return ui.Change
	case ActionDelete:
		return ui.Remove
	case ActionReconcile:
		return ui.Change
	case ActionNoop:
		return ui.Noop
	default:
		return ui.Info
	}
}

// FormatAction returns a human-readable action string
func (r Resource) FormatAction() string {
	switch r.Action {
	case ActionCreate:
		return "will be created"
	case ActionUpdate:
		return "will be updated"
	case ActionDelete:
		return "will be deleted"
	case ActionReconcile:
		if r.Details != "" {
			return fmt.Sprintf("will be reconciled (%s)", r.Details)
		}
		return "will be reconciled"
	case ActionNoop:
		if r.Details != "" {
			return r.Details
		}
		return "up-to-date"
	default:
		return string(r.Action)
	}
}

// PlanRenderOptions controls how a ResourcePlan is rendered.
type PlanRenderOptions struct {
	// Full renders all resources including no-ops. When false (changes-only mode,
	// wired in a later task) only resources with actions are shown.
	Full bool
}

// RenderResourcePlanOpts renders a ResourcePlan according to opts.
func RenderResourcePlanOpts(rp *ResourcePlan, opts PlanRenderOptions) string {
	if opts.Full {
		return renderResourcePlanFull(rp)
	}
	return renderResourcePlanChangesOnly(rp)
}

// RenderResourcePlan renders a ResourcePlan with consistent formatting.
// It is equivalent to RenderResourcePlanOpts with Full: true.
func RenderResourcePlan(rp *ResourcePlan) string {
	return RenderResourcePlanOpts(rp, PlanRenderOptions{Full: true})
}

func renderResourcePlanFull(rp *ResourcePlan) string {
	var sections []ui.NestedSection

	// Volumes section
	if len(rp.Volumes) > 0 {
		var items []ui.DiffLine
		for _, res := range rp.Volumes {
			name := ui.Italic(res.Name)
			msg := fmt.Sprintf("%s %s", name, res.FormatAction())
			items = append(items, ui.DiffLine{Type: res.ChangeType, Message: msg})
		}
		sections = append(sections, ui.NestedSection{Title: "Volumes", Items: items})
	}

	// Networks section
	if len(rp.Networks) > 0 {
		var items []ui.DiffLine
		for _, res := range rp.Networks {
			name := ui.Italic(res.Name)
			msg := fmt.Sprintf("%s %s", name, res.FormatAction())
			items = append(items, ui.DiffLine{Type: res.ChangeType, Message: msg})
		}
		sections = append(sections, ui.NestedSection{Title: "Networks", Items: items})
	}

	// Stacks section with nested services
	if len(rp.Stacks) > 0 {
		var stackSections []ui.NestedSection

		// Sort stack names for consistent output
		stackNames := make([]string, 0, len(rp.Stacks))
		for name := range rp.Stacks {
			stackNames = append(stackNames, name)
		}
		sort.Strings(stackNames)

		for _, stackName := range stackNames {
			services := rp.Stacks[stackName]
			var items []ui.DiffLine

			for _, res := range services {
				// For services, we don't repeat the stack name since it's in the section title
				name := ui.Italic(res.Name)
				msg := fmt.Sprintf("%s %s", name, res.FormatAction())
				items = append(items, ui.DiffLine{Type: res.ChangeType, Message: msg})
			}

			if len(items) > 0 {
				stackSections = append(stackSections, ui.NestedSection{Title: stackName, Items: items})
			}
		}

		if len(stackSections) > 0 {
			sections = append(sections, ui.NestedSection{
				Title:    "Stacks",
				Sections: stackSections,
			})
		}
	}

	// Filesets section with nested file changes
	if len(rp.Filesets) > 0 {
		var filesetSections []ui.NestedSection

		// Sort fileset names for consistent output
		filesetNames := make([]string, 0, len(rp.Filesets))
		for name := range rp.Filesets {
			filesetNames = append(filesetNames, name)
		}
		sort.Strings(filesetNames)

		for _, filesetName := range filesetNames {
			items := rp.Filesets[filesetName]
			var diffLines []ui.DiffLine

			for _, res := range items {
				var msg string
				if res.Action == ActionNoop {
					msg = res.Details
					if msg == "" {
						msg = "no file changes"
					}
				} else if res.Name != "" {
					// File-specific action
					fname := ui.Italic(res.Name)
					msg = fmt.Sprintf("%s %s", res.Action, fname)
				} else {
					// General fileset message
					msg = res.FormatAction()
				}
				diffLines = append(diffLines, ui.DiffLine{Type: res.ChangeType, Message: msg})
			}

			if len(diffLines) > 0 {
				filesetSections = append(filesetSections, ui.NestedSection{Title: filesetName, Items: diffLines})
			}
		}

		if len(filesetSections) > 0 {
			sections = append(sections, ui.NestedSection{
				Title:    "Filesets",
				Sections: filesetSections,
			})
		}
	}

	// Containers section (for orphaned containers)
	if len(rp.Containers) > 0 {
		var items []ui.DiffLine
		for _, res := range rp.Containers {
			name := ui.Italic(res.Name)
			msg := fmt.Sprintf("%s %s", name, res.FormatAction())
			items = append(items, ui.DiffLine{Type: res.ChangeType, Message: msg})
		}
		sections = append(sections, ui.NestedSection{Title: "Containers", Items: items})
	}

	// Calculate summary counts
	createCount, updateCount, deleteCount := rp.CountActions()

	// Render sections
	result := ui.RenderNestedSections(sections)

	// Add summary line
	if createCount > 0 || updateCount > 0 || deleteCount > 0 {
		if result != "" {
			result += "\n"
		}
		result += ui.FormatPlanSummary(createCount, updateCount, deleteCount)
	}

	return result
}

// countNoop returns the number of resources with ActionNoop.
func countNoop(rs []Resource) int {
	n := 0
	for _, r := range rs {
		if r.Action == ActionNoop {
			n++
		}
	}
	return n
}

// renderResourcePlanChangesOnly renders only changed resources, with a footer
// count of unchanged (no-op) resources per section.
// Stacks and Filesets changes-only rendering added in a later task.
func renderResourcePlanChangesOnly(rp *ResourcePlan) string {
	var sections []ui.NestedSection

	buildFlatSection := func(title string, resources []Resource) {
		if len(resources) == 0 {
			return
		}
		var items []ui.DiffLine
		for _, res := range resources {
			if res.Action == ActionNoop {
				continue
			}
			name := ui.Italic(res.Name)
			msg := fmt.Sprintf("%s %s", name, res.FormatAction())
			items = append(items, ui.DiffLine{Type: res.ChangeType, Message: msg})
		}
		sec := ui.NestedSection{Title: title, Items: items}
		noop := countNoop(resources)
		if noop > 0 {
			sec.Footer = []ui.DiffLine{{Type: ui.Info, Message: fmt.Sprintf("%d unchanged", noop)}}
		}
		sections = append(sections, sec)
	}

	buildFlatSection("Volumes", rp.Volumes)
	buildFlatSection("Networks", rp.Networks)
	buildFlatSection("Containers", rp.Containers)

	result := ui.RenderNestedSections(sections)

	create, update, delete := rp.CountActions()
	if create > 0 || update > 0 || delete > 0 {
		if result != "" {
			result += "\n"
		}
		result += ui.FormatPlanSummary(create, update, delete)
	}

	return result
}

// CountActions counts the number of each action type in the plan
func (rp *ResourcePlan) CountActions() (create, update, delete int) {
	countResource := func(res Resource) {
		switch res.Action {
		case ActionCreate:
			create++
		case ActionUpdate, ActionReconcile:
			update++
		case ActionDelete:
			delete++
		}
	}

	for _, res := range rp.Volumes {
		countResource(res)
	}
	for _, res := range rp.Networks {
		countResource(res)
	}
	for _, services := range rp.Stacks {
		for _, res := range services {
			countResource(res)
		}
	}
	for _, items := range rp.Filesets {
		for _, res := range items {
			// Only count actual file operations, not status messages
			if res.Name != "" && res.Action != ActionNoop {
				countResource(res)
			}
		}
	}
	for _, res := range rp.Containers {
		countResource(res)
	}

	return create, update, delete
}

// AllResources returns all resources from the plan as a flat list (for testing)
func (rp *ResourcePlan) AllResources() []Resource {
	var all []Resource

	if rp == nil {
		return all
	}

	all = append(all, rp.Volumes...)
	all = append(all, rp.Networks...)

	for _, services := range rp.Stacks {
		all = append(all, services...)
	}

	for _, items := range rp.Filesets {
		all = append(all, items...)
	}

	all = append(all, rp.Containers...)

	return all
}
