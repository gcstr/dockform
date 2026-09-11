package planner

import (
	"context"
	"regexp"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
)

// stackComposeProject returns the normalized compose project a stack runs under:
// the manifest's project name when set (dockform passes it to compose as -p),
// otherwise the name compose itself resolves from COMPOSE_PROJECT_NAME, the
// top-level name:, or the working directory. inline must be the environment
// compose up receives, since it can set COMPOSE_PROJECT_NAME or feed name:.
func stackComposeProject(ctx context.Context, client DockerClient, stack manifest.Stack, inline []string) (string, error) {
	if stack.Project != nil && stack.Project.Name != "" {
		return normalizeComposeProject(stack.Project.Name), nil
	}
	doc, err := client.ComposeConfigFull(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, inline)
	if err != nil {
		return "", apperr.Wrap("planner.stackComposeProject", apperr.External, err, "resolve compose project for stack %s", stack.Root)
	}
	name := normalizeComposeProject(doc.Name)
	if name == "" {
		return "", apperr.New("planner.stackComposeProject", apperr.External, "compose resolved no project name for stack %s", stack.Root)
	}
	return name, nil
}

var composeProjectChars = regexp.MustCompile(`[a-z0-9_-]`)

// normalizeComposeProject applies compose's project-name normalization
// (lowercase, keep [a-z0-9_-], trim leading "_" and "-") so a stack's name
// compares equal to the com.docker.compose.project label compose writes.
func normalizeComposeProject(s string) string {
	s = strings.ToLower(s)
	s = strings.Join(composeProjectChars.FindAllString(s, -1), "")
	return strings.TrimLeft(s, "_-")
}

// desiredStacks is what should be running on one context, used to tell an active
// stack's compose resources from those left behind by a removed stack.
type desiredStacks struct {
	// services holds every desired service name across all stacks.
	services map[string]struct{}
	// byProject maps each stack's resolved compose project to its services. It is
	// nil when any stack's project could not be resolved.
	byProject map[string]map[string]struct{}
}

func newDesiredStacks() *desiredStacks {
	return &desiredStacks{services: map[string]struct{}{}, byProject: map[string]map[string]struct{}{}}
}

// add records a stack's services under its project. An empty project means it
// could not be resolved, which makes the whole project view unknown.
func (d *desiredStacks) add(project string, services []string) {
	for _, s := range services {
		d.services[s] = struct{}{}
	}
	if project == "" {
		d.byProject = nil
		return
	}
	if d.byProject == nil {
		return
	}
	set := d.byProject[project]
	if set == nil {
		set = map[string]struct{}{}
		d.byProject[project] = set
	}
	for _, s := range services {
		set[s] = struct{}{}
	}
}

// projects returns the desired compose projects, or nil when unknown.
func (d *desiredStacks) projects() map[string]struct{} {
	if d.byProject == nil {
		return nil
	}
	out := make(map[string]struct{}, len(d.byProject))
	for p := range d.byProject {
		out[p] = struct{}{}
	}
	return out
}

// wantsContainer reports whether a compose container belongs to a desired stack.
// Without project information it falls back to matching the service name alone,
// which only ever keeps more containers.
func (d *desiredStacks) wantsContainer(project, service string) bool {
	if d.byProject == nil || project == "" {
		_, ok := d.services[service]
		return ok
	}
	_, ok := d.byProject[normalizeComposeProject(project)][service]
	return ok
}
