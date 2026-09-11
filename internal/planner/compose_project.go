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
