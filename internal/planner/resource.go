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
	ActionStart     Action = "start"
	ActionUpdate    Action = "update"
	ActionDelete    Action = "delete"
	ActionReconcile Action = "reconcile"
	ActionNoop      Action = "no-op"
	// ActionKeep marks a resource destroy leaves in place because the manifest
	// declares it with `destroy: false`. Shown like a change, counted like a
	// no-op: it is never a create, update or delete, and never pending work.
	ActionKeep Action = "keep"
)

// pending reports whether a resource with this action is work the run must do.
// A no-op and a kept resource are not.
func (a Action) pending() bool { return a != ActionNoop && a != ActionKeep }

// ResourceType represents the type of infrastructure resource
type ResourceType string

const (
	ResourceVolume    ResourceType = "volume"
	ResourceNetwork   ResourceType = "network"
	ResourceService   ResourceType = "service"
	ResourceContainer ResourceType = "container"
	ResourceFileset   ResourceType = "fileset"
	ResourceFile      ResourceType = "file" // Individual file in a fileset
	ResourceStack     ResourceType = "stack"
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
	case ActionStart:
		return ui.Add
	case ActionUpdate:
		return ui.Change
	case ActionDelete:
		return ui.Remove
	case ActionReconcile:
		return ui.Change
	case ActionNoop, ActionKeep:
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
	case ActionStart:
		return "will be started"
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
	case ActionKeep:
		if r.Details != "" {
			return r.Details
		}
		return "kept (destroy: false)"
	default:
		return string(r.Action)
	}
}

// filesetChangedFileCap is the maximum number of changed file lines shown per
// fileset in changes-only mode before the remainder is summarised.
const filesetChangedFileCap = 10

// PlanRenderOptions controls how a ResourcePlan is rendered.
type PlanRenderOptions struct {
	// Full renders the complete plan including unchanged resources; when false,
	// output is changes-only (only resources with pending actions are shown).
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
	sections := renderContextSections(rp, PlanRenderOptions{Full: true}, false)
	return appendPlanSummary(ui.RenderNestedSections(sections), rp)
}

// renderContextSections builds the section tree for ONE context's resources.
// It deliberately returns sections rather than a string so both the plan
// renderer and the apply view can wrap them with their own decoration.
//
// opts.Full selects between the two existing rendering shapes: the full
// inventory (every resource, including no-ops) used by renderResourcePlanFull,
// and the changes-only view (no-ops filtered out, "N unchanged" footers, and a
// per-fileset changed-file cap) used by renderResourcePlanChangesOnly.
//
// nested says whether the caller will wrap the returned sections under a
// context header (renderPlanByContext does). A fileset's key is
// "context/stack/volume" — needed for display when these sections stand
// alone (RenderResourcePlanOpts/RenderResourcePlanFull, called directly on a
// bare ResourcePlan with no context shown anywhere else), but redundant once
// nested under that same context's header, so nested trims it down to
// "stack/volume" via FilesetDisplayName.
func renderContextSections(rp *ResourcePlan, opts PlanRenderOptions, nested bool) []ui.NestedSection {
	if opts.Full {
		var sections []ui.NestedSection

		for _, title := range SectionOrder {
			switch title {
			case SectionTitle(ResourceVolume):
				// Volumes section
				if len(rp.Volumes) > 0 {
					var items []ui.DiffLine
					for _, res := range rp.Volumes {
						items = append(items, formatResourceLine(res))
					}
					sections = append(sections, ui.NestedSection{Title: title, Items: items})
				}

			case SectionTitle(ResourceNetwork):
				// Networks section
				if len(rp.Networks) > 0 {
					var items []ui.DiffLine
					for _, res := range rp.Networks {
						items = append(items, formatResourceLine(res))
					}
					sections = append(sections, ui.NestedSection{Title: title, Items: items})
				}

			case SectionTitle(ResourceStack):
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
							items = append(items, formatResourceLine(res))
						}

						if len(items) > 0 {
							stackSections = append(stackSections, ui.NestedSection{Title: stackName, Items: items})
						}
					}

					if len(stackSections) > 0 {
						sections = append(sections, ui.NestedSection{
							Title:    title,
							Sections: stackSections,
						})
					}
				}

			case SectionTitle(ResourceFileset):
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

						title := filesetName
						if nested {
							title = FilesetDisplayName(filesetName)
						}
						if len(diffLines) > 0 {
							filesetSections = append(filesetSections, ui.NestedSection{Title: title, Items: diffLines})
						}
					}

					if len(filesetSections) > 0 {
						sections = append(sections, ui.NestedSection{
							Title:    title,
							Sections: filesetSections,
						})
					}
				}

			case SectionTitle(ResourceContainer):
				// Containers section (for orphaned containers)
				if len(rp.Containers) > 0 {
					var items []ui.DiffLine
					for _, res := range rp.Containers {
						items = append(items, formatResourceLine(res))
					}
					sections = append(sections, ui.NestedSection{Title: title, Items: items})
				}
			}
		}

		return sections
	}

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
			items = append(items, formatResourceLine(res))
		}
		// Nothing pending here: the whole section is noise in a view whose
		// job is to show what will change. --long still lists it.
		if len(items) == 0 {
			return
		}
		sections = append(sections, ui.NestedSection{Title: title, Items: items})
	}

	for _, title := range SectionOrder {
		switch title {
		case SectionTitle(ResourceVolume):
			buildFlatSection(title, rp.Volumes)

		case SectionTitle(ResourceNetwork):
			buildFlatSection(title, rp.Networks)

		case SectionTitle(ResourceStack):
			// Stacks section (changes-only)
			if len(rp.Stacks) > 0 {
				stackNames := make([]string, 0, len(rp.Stacks))
				for name := range rp.Stacks {
					stackNames = append(stackNames, name)
				}
				sort.Strings(stackNames)

				var changedStackSections []ui.NestedSection
				unchangedServices := 0

				for _, stackName := range stackNames {
					services := rp.Stacks[stackName]
					unchangedServices += countNoop(services)

					var items []ui.DiffLine
					for _, svc := range services {
						if svc.Action != ActionNoop {
							items = append(items, formatResourceLine(svc))
						}
					}
					if len(items) > 0 {
						changedStackSections = append(changedStackSections, ui.NestedSection{Title: stackName, Items: items})
					}
				}

				if len(changedStackSections) == 0 {
					break
				}
				sections = append(sections, ui.NestedSection{Title: title, Sections: changedStackSections})
			}

		case SectionTitle(ResourceFileset):
			// Filesets section (changes-only)
			if len(rp.Filesets) > 0 {
				filesetNames := make([]string, 0, len(rp.Filesets))
				for name := range rp.Filesets {
					filesetNames = append(filesetNames, name)
				}
				sort.Strings(filesetNames)

				var changedFilesetSections []ui.NestedSection
				unchangedFilesets := 0

				for _, filesetName := range filesetNames {
					items := rp.Filesets[filesetName]
					if countNoop(items) == len(items) {
						unchangedFilesets++
						continue
					}

					var changedFiles []Resource
					for _, item := range items {
						if item.Action != ActionNoop {
							changedFiles = append(changedFiles, item)
						}
					}

					show := min(filesetChangedFileCap, len(changedFiles))

					var diffLines []ui.DiffLine
					for _, item := range changedFiles[:show] {
						diffLines = append(diffLines, formatFilesetItem(item))
					}

					if len(changedFiles) > filesetChangedFileCap {
						remaining := changedFiles[filesetChangedFileCap:]
						extra := len(remaining)
						c, u, d := summarizeFileActions(remaining)
						diffLines = append(diffLines, ui.DiffLine{
							Type:    ui.Info,
							Message: fmt.Sprintf("… and %d more changed (%d created, %d updated, %d deleted)", extra, c, u, d),
						})
					}

					title := filesetName
					if nested {
						title = FilesetDisplayName(filesetName)
					}
					changedFilesetSections = append(changedFilesetSections, ui.NestedSection{Title: title, Items: diffLines})
				}

				if len(changedFilesetSections) == 0 {
					break
				}
				sections = append(sections, ui.NestedSection{Title: title, Sections: changedFilesetSections})
			}

		case SectionTitle(ResourceContainer):
			buildFlatSection(title, rp.Containers)
		}
	}

	return sections
}

// renderPlanByContext renders every context in sorted order, each as a section
// whose children are that context's resource sections. The context level always
// appears, including for a single context, so there is one output shape.
func renderPlanByContext(byContext map[string]*ContextPlan, opts PlanRenderOptions) string {
	var sections []ui.NestedSection
	for _, name := range sortedKeys(byContext) {
		cp := byContext[name]
		if cp == nil || cp.Resources == nil {
			continue
		}
		inner := renderContextSections(cp.Resources, opts, true)
		if len(inner) == 0 {
			continue
		}
		sections = append(sections, ui.NestedSection{Title: name, Sections: inner})
	}
	// This tree has one more wrapping level than a plain ResourcePlan's (the
	// context itself), so the resource-type sections nested under each
	// context need RenderGroupedNestedSections' extra level of blank-line
	// separation to keep the spacing they had before context-wrapping
	// existed. RenderNestedSections stays reserved for the plain, un-wrapped
	// shape (e.g. destroy's direct RenderResourcePlanOpts calls).
	return ui.RenderGroupedNestedSections(sections)
}

// formatResourceLine returns a DiffLine for a resource using the standard
// "italic-name action-text" format used by Volumes, Networks, Containers, and
// Stacks flat items.
func formatResourceLine(res Resource) ui.DiffLine {
	return ui.DiffLine{
		Type:    res.ChangeType,
		Message: fmt.Sprintf("%s %s", ui.Italic(res.Name), res.FormatAction()),
	}
}

// appendPlanSummary appends a plan summary line to result when there are any
// creates, updates, or deletes.
func appendPlanSummary(result string, rp *ResourcePlan) string {
	create, update, delete := rp.CountActions()
	if create > 0 || update > 0 || delete > 0 {
		if result != "" {
			result += "\n"
		}
		result += ui.FormatPlanSummary(create, update, delete)
	}
	return result
}

// formatFilesetItem formats a CHANGED fileset file line. Callers must
// pre-filter ActionNoop items (the no-op "no file changes" case is handled by
// the full renderer).
func formatFilesetItem(res Resource) ui.DiffLine {
	var msg string
	if res.Name != "" {
		msg = fmt.Sprintf("%s %s", res.Action, ui.Italic(res.Name))
	} else {
		msg = res.FormatAction()
	}
	return ui.DiffLine{Type: res.ChangeType, Message: msg}
}

// summarizeFileActions counts creates, updates, and deletes in a slice of Resources.
func summarizeFileActions(rs []Resource) (create, update, delete int) {
	for _, r := range rs {
		switch r.Action {
		case ActionCreate:
			create++
		case ActionUpdate, ActionReconcile:
			update++
		case ActionDelete:
			delete++
		}
	}
	return
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

// totalUnits counts the resources a plan tracks, for the all-clear message.
// Filesets are counted per-fileset (one unit each), matching how the changes-only
// renderer treats a fileset as a single no-op unit.
func totalUnits(rp *ResourcePlan) int {
	n := len(rp.Volumes) + len(rp.Networks) + len(rp.Containers) + len(rp.Filesets)
	for _, services := range rp.Stacks {
		n += len(services)
	}
	return n
}

// renderResourcePlanChangesOnly renders only changed resources, with a footer
// count of unchanged (no-op) resources per section.
func renderResourcePlanChangesOnly(rp *ResourcePlan) string {
	if c, u, d := rp.CountActions(); c == 0 && u == 0 && d == 0 {
		return fmt.Sprintf("No changes. %d resources up to date.", totalUnits(rp))
	}

	sections := renderContextSections(rp, PlanRenderOptions{Full: false}, false)
	return appendPlanSummary(ui.RenderNestedSections(sections), rp)
}

// hasAnyResources reports whether a ResourcePlan tracks anything at all
// (regardless of action), for the "nothing to do" placeholder.
func hasAnyResources(rp *ResourcePlan) bool {
	return len(rp.Volumes) > 0 || len(rp.Networks) > 0 ||
		len(rp.Stacks) > 0 || len(rp.Filesets) > 0 || len(rp.Containers) > 0
}

// CountActions counts the number of each action type in the plan
func (rp *ResourcePlan) CountActions() (create, update, delete int) {
	countResource := func(res Resource) {
		switch res.Action {
		case ActionCreate, ActionStart:
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
