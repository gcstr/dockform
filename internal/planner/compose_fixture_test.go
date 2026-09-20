package planner

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
)

// composeFixture replays a real `docker compose --progress json` capture from
// internal/dockercli/testdata/composeprogress. See that directory's README.
func composeFixture(t *testing.T, name string) []dockercli.ComposeEvent {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "dockercli", "testdata", "composeprogress", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var events []dockercli.ComposeEvent
	for _, line := range bytes.Split(data, []byte("\n")) {
		if ev, ok := dockercli.ParseComposeEvent(line); ok {
			events = append(events, ev)
		}
	}
	if len(events) == 0 {
		t.Fatalf("fixture %s produced no events", name)
	}
	return events
}
