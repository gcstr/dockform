package dockercli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureLines returns the non-blank lines of a real compose --progress json
// capture from testdata/composeprogress. See that directory's README for what
// each capture contains and what it established.
func fixtureLines(t *testing.T, name string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "composeprogress", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var lines [][]byte
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

func parseFixture(t *testing.T, name string) []ComposeEvent {
	t.Helper()
	var events []ComposeEvent
	for _, line := range fixtureLines(t, name) {
		ev, ok := ParseComposeEvent(line)
		if !ok {
			t.Fatalf("%s: line did not decode: %s", name, line)
		}
		events = append(events, ev)
	}
	return events
}

// A schema drift canary: every line compose actually emitted must decode.
func TestParseComposeEvent_EveryCapturedLineDecodes(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "composeprogress"))
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}
	seen := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		seen++
		parseFixture(t, e.Name())
	}
	if seen != 11 {
		t.Fatalf("expected 11 fixtures, found %d", seen)
	}
}

func TestParseComposeEvent_Container(t *testing.T) {
	ev := parseFixture(t, "create.jsonl")[2]
	want := ComposeEvent{Kind: EventContainer, Name: "skqfixture-alpha-1", Status: "Working", Text: "Creating"}
	if ev != want {
		t.Fatalf("got %+v, want %+v", ev, want)
	}
}

// With container_name set, the event carries no trace of the service.
func TestParseComposeEvent_ContainerNameOverride(t *testing.T) {
	ev := parseFixture(t, "container_name_override.jsonl")[2]
	if ev.Kind != EventContainer || ev.Name != "totally-custom-name" {
		t.Fatalf("got %+v", ev)
	}
}

func TestParseComposeEvent_Network(t *testing.T) {
	ev := parseFixture(t, "create.jsonl")[0]
	if ev.Kind != EventNetwork || ev.Name != "skqfixture_default" {
		t.Fatalf("got %+v", ev)
	}
}

func TestParseComposeEvent_ImageAndLayer(t *testing.T) {
	events := parseFixture(t, "pull_with_layers.jsonl")
	if img := events[0]; img.Kind != EventImage || img.Name != "alpine:3.19" || img.Text != "Pulling" {
		t.Fatalf("image event: got %+v", img)
	}
	layer := events[2]
	want := ComposeEvent{Kind: EventLayer, Name: "5711127a7748", Image: "alpine:3.19",
		Status: "Working", Text: "Downloading", Current: 48537, Total: 3359301}
	if layer != want {
		t.Fatalf("layer event: got %+v, want %+v", layer, want)
	}
}

// A layer already on disk reports "Already exists" with a null total.
func TestParseComposeEvent_AlreadyExistsHasNoTotal(t *testing.T) {
	for _, ev := range parseFixture(t, "pull_shared_base_layer.jsonl") {
		if ev.Text == "Already exists" {
			if ev.Kind != EventLayer || ev.Total != 0 {
				t.Fatalf("got %+v, want a layer with Total 0", ev)
			}
			return
		}
	}
	t.Fatal("fixture has no Already exists event")
}

// The terminal error line has a different shape: no id, status or text.
func TestParseComposeEvent_ErrorShape(t *testing.T) {
	events := parseFixture(t, "error.jsonl")
	last := events[len(events)-1]
	if last.Kind != EventError {
		t.Fatalf("got %+v, want EventError", last)
	}
	if !strings.HasPrefix(last.Message, "Error response from daemon: pull access denied") {
		t.Fatalf("message = %q", last.Message)
	}
}

func TestParseComposeEvent_RejectsUndecodableAndUnrecognised(t *testing.T) {
	for _, line := range []string{
		"",
		"not json",
		`{}`,
		`{"id":"Volume data","status":"Done","text":"Created"}`,
		`{"id":"abc123","parent_id":"not-an-image","status":"Working","text":"Downloading"}`,
		`Connection closed by 10.0.0.1 port 22`,
	} {
		if ev, ok := ParseComposeEvent([]byte(line)); ok {
			t.Errorf("ParseComposeEvent(%q) = %+v, true; want false", line, ev)
		}
	}
}
