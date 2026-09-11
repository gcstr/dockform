package planner

import (
	"context"
	"errors"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

// A failed remote call must surface as an error, never as a plausible state:
// "nothing running" plans creates for a live stack, "drifted" plans recreates,
// and a missing desired hash hides real drift.

func TestGetRunningServices_ComposePsFailureIsAnError(t *testing.T) {
	mock := newMockDocker()
	mock.composePsError = errors.New("ssh: session refused")
	d := NewServiceStateDetector(mock)
	if _, err := d.GetRunningServices(context.Background(), manifest.Stack{Root: "/srv/app"}, nil); err == nil {
		t.Fatal("expected an error, got an empty running set")
	}
}

func TestDetectServiceState_HashFailureIsAnError(t *testing.T) {
	mock := newMockDocker()
	mock.composeConfigHashError = errors.New("ssh: session refused")
	d := NewServiceStateDetector(mock)
	running := map[string]dockercli.ComposePsItem{"web": {Name: "app-web-1", Service: "web"}}
	if _, err := d.DetectServiceState(context.Background(), "web", "app", manifest.Stack{Root: "/srv/app"}, "demo", nil, running); err == nil {
		t.Fatal("expected an error, got a state computed without a desired hash")
	}
}

func TestDetectServiceState_LabelInspectFailureIsAnError(t *testing.T) {
	mock := newMockDocker()
	mock.inspectLabelsError = errors.New("ssh: session refused")
	d := NewServiceStateDetector(mock)
	running := map[string]dockercli.ComposePsItem{"web": {Name: "app-web-1", Service: "web"}}
	info, err := d.DetectServiceState(context.Background(), "web", "app", manifest.Stack{Root: "/srv/app"}, "demo", nil, running)
	if err == nil {
		t.Fatalf("expected an error, got state %v", info.State)
	}
}
