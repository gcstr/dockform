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
	got, err := resolveTargetServices(context.Background(), nil, fs)
	if err != nil {
		t.Fatalf("resolve explicit list: %v", err)
	}
	want := []string{"web", "api"}
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
	_, err := resolveTargetServices(context.Background(), nil, fs)
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
	_, err := resolveTargetServices(context.Background(), mockDocker, fs)
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
	_, err := resolveTargetServices(context.Background(), mockDocker, fs)
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
	got, err := resolveTargetServices(context.Background(), mockDocker, fs)
	if err != nil {
		t.Fatalf("resolve attached services: %v", err)
	}
	want := []string{"api", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected services: got=%#v want=%#v", got, want)
	}
}
