package planner

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

func TestResolveTargetServices_ExplicitListDedupesAndSkipsEmpty(t *testing.T) {
	fs := manifest.FilesetSpec{
		RestartServices: manifest.RestartTargets{
			Services: []string{"web", "", "api", "web"},
		},
	}
	got, err := resolveTargetServices(context.Background(), nil, fs, nil)
	if err != nil {
		t.Fatalf("resolve explicit list: %v", err)
	}
	want := []restartTarget{{Service: "web"}, {Service: "api"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected services: got=%#v want=%#v", got, want)
	}
}

func TestResolveTargetServices_AttachedRequiresDocker(t *testing.T) {
	fs := manifest.FilesetSpec{
		TargetVolume: "data",
		RestartServices: manifest.RestartTargets{
			Attached: true,
		},
	}
	_, err := resolveTargetServices(context.Background(), nil, fs, nil)
	if err == nil {
		t.Fatalf("expected precondition error")
	}
	if !apperr.IsKind(err, apperr.Precondition) {
		t.Fatalf("expected precondition error kind, got: %v", err)
	}
}

func TestResolveTargetServices_AttachedVolumeLookupError(t *testing.T) {
	mockDocker := newMockDocker()
	mockDocker.listContainersUsingVolError = errors.New("volume lookup failed")

	fs := manifest.FilesetSpec{
		TargetVolume: "data",
		RestartServices: manifest.RestartTargets{
			Attached: true,
		},
	}
	_, err := resolveTargetServices(context.Background(), mockDocker, fs, nil)
	if err == nil {
		t.Fatalf("expected attached lookup error")
	}
	if !strings.Contains(err.Error(), "list containers using volume data") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveTargetServices_AttachedComposeLookupError(t *testing.T) {
	mockDocker := newMockDocker()
	mockDocker.containersUsingVolume = []string{"demo-web-1"}
	mockDocker.listComposeContainersError = errors.New("compose list failed")

	fs := manifest.FilesetSpec{
		TargetVolume: "data",
		RestartServices: manifest.RestartTargets{
			Attached: true,
		},
	}
	_, err := resolveTargetServices(context.Background(), mockDocker, fs, nil)
	if err == nil {
		t.Fatalf("expected compose lookup error")
	}
	if !strings.Contains(err.Error(), "list compose containers for volume data") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveTargetServices_AttachedResolvesAndSortsServices(t *testing.T) {
	mockDocker := newMockDocker()
	mockDocker.containersUsingVolume = []string{"demo-api-1", "demo-web-1", "orphan"}
	mockDocker.containers = []dockercli.PsBrief{
		{Project: "demo", Service: "web", Name: "demo-web-1"},
		{Project: "demo", Service: "api", Name: "demo-api-1"},
		{Project: "demo", Service: "db", Name: "demo-db-1"},
	}

	fs := manifest.FilesetSpec{
		TargetVolume: "data",
		RestartServices: manifest.RestartTargets{
			Attached: true,
		},
	}
	got, err := resolveTargetServices(context.Background(), mockDocker, fs, nil)
	if err != nil {
		t.Fatalf("resolve attached services: %v", err)
	}
	want := []restartTarget{{Service: "api"}, {Service: "web"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected services: got=%#v want=%#v", got, want)
	}
}

// An explicit restart_services list names services of the fileset's OWN stack,
// so the fileset's Stack is what must reach the target.
func TestResolveTargetServices_ExplicitListCarriesFilesetStack(t *testing.T) {
	fs := manifest.FilesetSpec{
		Stack: "traefik",
		RestartServices: manifest.RestartTargets{
			Services: []string{"proxy"},
		},
	}
	got, err := resolveTargetServices(context.Background(), nil, fs, nil)
	if err != nil {
		t.Fatalf("resolve explicit list: %v", err)
	}
	want := []restartTarget{{Stack: "traefik", Service: "proxy"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected targets: got=%#v want=%#v", got, want)
	}
}

// A fileset merged in from the declared manifest (validation.go) stores fs.Stack
// as a "context/stack" key, not the bare stack name loader.go's directory
// discovery uses. SeedRefs' Parent is always the bare name, so the explicit-list
// branch must reduce a keyed value to its stack part or every manifest-declared
// fileset produces a Parent that never matches the seeded line.
func TestResolveTargetServices_ExplicitListReducesKeyedFilesetStack(t *testing.T) {
	fs := manifest.FilesetSpec{
		Stack: "hetzner-two/traefik",
		RestartServices: manifest.RestartTargets{
			Services: []string{"proxy"},
		},
	}
	got, err := resolveTargetServices(context.Background(), nil, fs, nil)
	if err != nil {
		t.Fatalf("resolve explicit list: %v", err)
	}
	want := []restartTarget{{Stack: "traefik", Service: "proxy"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected targets: got=%#v want=%#v", got, want)
	}
}

// Attached discovery finds services from any container mounting the volume,
// which may belong to a different stack than the fileset. The container's own
// compose project decides the stack — using the fileset's would point the apply
// view at another stack's line.
func TestResolveTargetServices_AttachedUsesContainerProjectNotFilesetStack(t *testing.T) {
	mockDocker := newMockDocker()
	mockDocker.containersUsingVolume = []string{"coredns-coredns-1"}
	mockDocker.containers = []dockercli.PsBrief{
		{Project: "coredns", Service: "coredns", Name: "coredns-coredns-1"},
	}

	fs := manifest.FilesetSpec{
		Stack:           "traefik", // deliberately NOT the container's stack
		TargetVolume:    "data",
		RestartServices: manifest.RestartTargets{Attached: true},
	}
	got, err := resolveTargetServices(context.Background(), mockDocker, fs,
		map[string]string{"coredns": "coredns"})
	if err != nil {
		t.Fatalf("resolve attached services: %v", err)
	}
	want := []restartTarget{{Stack: "coredns", Service: "coredns"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected targets: got=%#v want=%#v", got, want)
	}
}

// A project with no manifest stack gets an empty Stack. One duplicate line is
// better than a line resolving to some other stack.
func TestResolveTargetServices_AttachedUnmappableProjectLeavesStackEmpty(t *testing.T) {
	mockDocker := newMockDocker()
	mockDocker.containersUsingVolume = []string{"sidecar-1"}
	mockDocker.containers = []dockercli.PsBrief{
		{Project: "some-unmanaged-project", Service: "sidecar", Name: "sidecar-1"},
	}

	fs := manifest.FilesetSpec{
		Stack:           "traefik",
		TargetVolume:    "data",
		RestartServices: manifest.RestartTargets{Attached: true},
	}
	got, err := resolveTargetServices(context.Background(), mockDocker, fs,
		map[string]string{"coredns": "coredns"})
	if err != nil {
		t.Fatalf("resolve attached services: %v", err)
	}
	want := []restartTarget{{Stack: "", Service: "sidecar"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected targets: got=%#v want=%#v", got, want)
	}
}

// Two stacks resolving to the same compose project are ambiguous; neither is
// mapped, so neither can steal the other's line.
func TestStackProjectMap_AmbiguousProjectMapsToNeither(t *testing.T) {
	mockDocker := newMockDocker()
	stacks := map[string]manifest.Stack{
		"a": {Root: "/stacks/dup", Project: &manifest.Project{Name: "dup"}},
		"b": {Root: "/stacks/other", Project: &manifest.Project{Name: "dup"}},
	}
	got := stackProjectMap(context.Background(), mockDocker, "ctx", stacks, nil)
	if _, ok := got["dup"]; ok {
		t.Fatalf("ambiguous project must not map to a stack; got=%#v", got)
	}
}

// A non-ambiguous stack must actually map to its own key. Without this, a stub
// `func stackProjectMap(...) map[string]string { return nil }` would satisfy
// every other test in this file.
func TestStackProjectMap_NonAmbiguousStackMapsToItsKey(t *testing.T) {
	mockDocker := newMockDocker()
	stacks := map[string]manifest.Stack{
		"web": {Root: "/stacks/web", Project: &manifest.Project{Name: "web"}},
	}
	got := stackProjectMap(context.Background(), mockDocker, "ctx", stacks, nil)
	if got["web"] != "web" {
		t.Fatalf("expected project %q to map to stack %q; got=%#v", "web", "web", got)
	}
}

func TestStackProjectMap_NoStacksReturnsNil(t *testing.T) {
	got := stackProjectMap(context.Background(), newMockDocker(), "ctx", nil, nil)
	if got != nil {
		t.Fatalf("expected nil map for zero stacks; got=%#v", got)
	}
}

// A stack with no project.name forces a real ComposeConfigFull call (unlike
// the ambiguous fixture above, whose Project.Name is set and short-circuits at
// compose_project.go:19 before docker is ever touched). When that call fails,
// the stack must be silently absent, not present with an empty value.
func TestStackProjectMap_ComposeConfigFailureExcludesStack(t *testing.T) {
	mockDocker := newMockDocker()
	mockDocker.composeConfigFullError = errors.New("compose config failed")
	stacks := map[string]manifest.Stack{
		"web": {Root: "/stacks/web"}, // no Project.Name
	}
	got := stackProjectMap(context.Background(), mockDocker, "ctx", stacks, nil)
	if len(got) != 0 {
		t.Fatalf("expected no entries when compose config fails; got=%#v", got)
	}
}
