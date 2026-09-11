package imagescmd

import (
	"context"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

// The running container is found by its com.docker.compose.project label, so the
// lookup must use the project compose actually ran the stack under, not a guess
// from the directory name.
func TestComposeProject(t *testing.T) {
	cases := []struct {
		name  string
		stack manifest.Stack
		doc   dockercli.ComposeConfigDoc
		want  string
	}{
		{"manifest project wins (dockform passes -p)", manifest.Stack{RootAbs: "/srv/app", Project: &manifest.Project{Name: "Explicit"}}, dockercli.ComposeConfigDoc{Name: "custom"}, "explicit"},
		{"name resolved by compose", manifest.Stack{RootAbs: "/srv/app"}, dockercli.ComposeConfigDoc{Name: "custom"}, "custom"},
		{"older compose without a name falls back to the directory", manifest.Stack{RootAbs: "/srv/My.App"}, dockercli.ComposeConfigDoc{}, "my.app"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := composeProject(tt.stack, tt.doc); got != tt.want {
				t.Fatalf("composeProject = %q, want %q", got, tt.want)
			}
		})
	}
}

// buildCheckInputs already renders each stack with compose, so it records the
// project compose resolved (here from a top-level name:) for the digest lookup.
func TestBuildCheckInputs_RecordsResolvedProject(t *testing.T) {
	dir := t.TempDir() // directory name differs from the resolved project
	cfg := &manifest.Config{Stacks: map[string]manifest.Stack{
		"local/app": {Root: dir, RootAbs: dir, Files: []string{"compose.yaml"}},
	}}
	client := &recordingComposeClient{configDoc: dockercli.ComposeConfigDoc{
		Name:     "custom",
		Services: map[string]dockercli.ComposeService{"web": {Image: "nginx:1.27"}},
	}}
	inputs, err := buildCheckInputs(context.Background(), cfg, func(string) composeClient { return client })
	if err != nil {
		t.Fatalf("buildCheckInputs: %v", err)
	}
	if len(inputs) != 1 || inputs[0].Project != "custom" {
		t.Fatalf("inputs = %+v, want one input with Project custom", inputs)
	}
	if got := projectsByStack(inputs); got["local/app"] != "custom" {
		t.Fatalf("projectsByStack = %v, want local/app -> custom", got)
	}
}
