package dockercli

import (
	"encoding/json"
	"strings"
)

// EventKind classifies one line of `docker compose --progress json` output.
type EventKind int

const (
	EventContainer EventKind = iota
	EventNetwork
	EventImage
	EventLayer
	EventError
)

// ComposeEvent is one decoded line of `docker compose --progress json` output.
//
// That output is not a documented stable compose API. Its shape is pinned by
// real captures in testdata/composeprogress, whose README records what was
// observed.
type ComposeEvent struct {
	Kind    EventKind
	Name    string // container name, network name, image ref, or layer id
	Image   string // EventLayer only: the image ref the layer belongs to
	Status  string // Working, Done, Error
	Text    string // Creating, Recreated, Waiting, Healthy, Downloading, ...
	Current int64  // layer byte progress, when present
	Total   int64
	Message string // EventError only: the human-readable cause
}

type rawComposeEvent struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	Status   string `json:"status"`
	Text     string `json:"text"`
	Current  int64  `json:"current"`
	Total    int64  `json:"total"`
	Error    bool   `json:"error"`
	Message  string `json:"message"`
}

var composeIDPrefixes = []struct {
	prefix string
	kind   EventKind
}{
	{"Container ", EventContainer},
	{"Network ", EventNetwork},
	{"Image ", EventImage},
}

// ParseComposeEvent decodes one line. It returns false for anything it cannot
// decode or does not recognise, and never an error: progress is advisory, so a
// change in compose's output must cost display detail, never an apply.
func ParseComposeEvent(line []byte) (ComposeEvent, bool) {
	var raw rawComposeEvent
	if err := json.Unmarshal(line, &raw); err != nil {
		return ComposeEvent{}, false
	}
	if raw.Error {
		// The terminal error line has a different shape: no id, status or text.
		return ComposeEvent{Kind: EventError, Status: "Error", Message: raw.Message}, true
	}
	if raw.ID == "" {
		return ComposeEvent{}, false
	}
	ev := ComposeEvent{Status: raw.Status, Text: raw.Text, Current: raw.Current, Total: raw.Total}
	if raw.ParentID != "" {
		image, ok := strings.CutPrefix(raw.ParentID, "Image ")
		if !ok {
			return ComposeEvent{}, false
		}
		ev.Kind, ev.Name, ev.Image = EventLayer, raw.ID, image
		return ev, true
	}
	for _, p := range composeIDPrefixes {
		if name, ok := strings.CutPrefix(raw.ID, p.prefix); ok {
			ev.Kind, ev.Name = p.kind, name
			return ev, true
		}
	}
	return ComposeEvent{}, false
}
