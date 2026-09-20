package dockercli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

func TestComposeUpWithProgress_DeliversEventsAndAddsFlag(t *testing.T) {
	f := &fakeExec{outConfigYAML: secretDoc, progressLines: fixtureLines(t, "create.jsonl")}
	c := &Client{exec: f, identifier: "demo"}
	var events []ComposeEvent
	if _, err := c.ComposeUpWithProgress(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", nil,
		func(ev ComposeEvent) { events = append(events, ev) }); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	if len(events) != 10 {
		t.Fatalf("got %d events, want 10", len(events))
	}
	p := indexOf(f.lastArgs, "--progress")
	if p < 0 || f.lastArgs[p+1] != "json" || p > indexOf(f.lastArgs, "up") {
		t.Fatalf("expected --progress json before up, got %#v", f.lastArgs)
	}
	// The labeled document — which holds decrypted secrets — must still go over stdin.
	assertPipedDocument(t, f)
}

func TestComposeUpWithProgress_ErrorEventBecomesReadableMessage(t *testing.T) {
	jsonStderr := `{"id":"Image dockform-nonexistent-image:v0","status":"Error"}`
	f := &fakeExec{outConfigYAML: secretDoc, progressLines: fixtureLines(t, "error.jsonl"), errUp: errors.New(jsonStderr)}
	c := &Client{exec: f, identifier: "demo"}
	_, err := c.ComposeUpWithProgress(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", nil, func(ComposeEvent) {})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "pull access denied") {
		t.Fatalf("error should carry compose's message, got %q", err)
	}
	if strings.Contains(err.Error(), `{"id"`) {
		t.Fatalf("error should not show JSON, got %q", err)
	}
}

func TestComposeUpWithProgress_FallsBackWhenUnsupported(t *testing.T) {
	f := &fakeExec{outConfigYAML: secretDoc, progressLines: fixtureLines(t, "create.jsonl"), errProgressProbe: errors.New("unsupported --progress value")}
	c := &Client{exec: f, identifier: "demo"}
	called := false
	if _, err := c.ComposeUpWithProgress(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", nil,
		func(ComposeEvent) { called = true }); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	if indexOf(f.lastArgs, "--progress") >= 0 {
		t.Fatalf("unsupported compose must not get --progress, got %#v", f.lastArgs)
	}
	if called {
		t.Fatal("onEvent must not be called when progress is unsupported")
	}
	assertPipedDocument(t, f)
}

func TestComposeUpWithProgress_CachesSupport(t *testing.T) {
	f := &fakeExec{outConfigYAML: secretDoc}
	c := &Client{exec: f, identifier: "demo"}
	for range 2 {
		if _, err := c.ComposeUpWithProgress(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", nil, func(ComposeEvent) {}); err != nil {
			t.Fatalf("compose up: %v", err)
		}
	}
	if f.progressProbeCalls != 1 {
		t.Fatalf("probe ran %d times, want 1", f.progressProbeCalls)
	}
}

// A failed probe may be a broken compose file or a transient failure, so it is
// not remembered: the next stack asks again.
func TestComposeUpWithProgress_DoesNotCacheUnsupported(t *testing.T) {
	f := &fakeExec{outConfigYAML: secretDoc, errProgressProbe: errors.New("no")}
	c := &Client{exec: f, identifier: "demo"}
	for range 2 {
		_, _ = c.ComposeUpWithProgress(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", nil, func(ComposeEvent) {})
	}
	if f.progressProbeCalls != 2 {
		t.Fatalf("probe ran %d times, want 2", f.progressProbeCalls)
	}
}
