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

// On an SSH retry, StderrLine also receives the failed attempt's lines, which
// can include a real compose error event that did not cause the final
// failure. The returned message must come from the final attempt's stderr
// (which here has no error event at all), never from an earlier one.
func TestComposeUpWithProgress_IgnoresStaleErrorFromEarlierAttempt(t *testing.T) {
	staleMsg := "Error response from daemon: pull access denied for stale-image"
	finalStderr := "Connection closed by 10.0.0.1 port 22"
	f := &fakeExec{
		outConfigYAML: secretDoc,
		progressLines: [][]byte{[]byte(`{"error":true,"message":"` + staleMsg + `"}`)},
		finalStderr:   &finalStderr,
		errUp:         errors.New("exit status 255"),
	}
	c := &Client{exec: f, identifier: "demo"}
	_, err := c.ComposeUpWithProgress(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", nil, func(ComposeEvent) {})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), staleMsg) {
		t.Fatalf("error must not carry an earlier attempt's message, got %q", err)
	}
}

// When the failing attempt reports no compose error event at all — an SSH
// drop, a killed process — the plain-text stderr line must be what the user
// sees, not raw JSON: --progress json must not be a regression from ComposeUp's
// plain-text stderr in the same failure.
func TestComposeUpWithProgress_NoErrorEventSurfacesPlainTextStderr(t *testing.T) {
	plainText := "Connection closed by 10.0.0.1 port 22"
	f := &fakeExec{
		outConfigYAML: secretDoc,
		finalStderr:   &plainText,
		errUp:         errors.New("exit status 255"),
	}
	c := &Client{exec: f, identifier: "demo"}
	_, err := c.ComposeUpWithProgress(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", nil, func(ComposeEvent) {})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), plainText) {
		t.Fatalf("error should surface the plain-text stderr, got %q", err)
	}
	if strings.Contains(err.Error(), `{"`) {
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
