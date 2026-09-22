package dockercli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
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

// Every user-facing printer (internal/cli/root.go) reports apperr.DeepestMessage,
// not err.Error() — which on an *apperr.E collapses to Op+Msg and drops the
// chain. With --progress json the Exec error's Msg IS the raw JSON stderr, so
// the readable message has to be the DEEPEST one or the user sees a wall of
// JSON instead.
func TestComposeUpWithProgress_ErrorEventBecomesReadableMessage(t *testing.T) {
	f := &fakeExec{outConfigYAML: secretDoc, progressLines: fixtureLines(t, "error.jsonl"), errUp: errors.New("exit status 1")}
	c := &Client{exec: f, identifier: "demo"}
	_, err := c.ComposeUpWithProgress(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", nil, func(ComposeEvent) {})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := apperr.DeepestMessage(err)
	if !strings.Contains(msg, "pull access denied") {
		t.Fatalf("error should carry compose's message, got %q", msg)
	}
	if strings.Contains(msg, `{"`) {
		t.Fatalf("error should not show JSON, got %q", msg)
	}
}

// A compose release that reports the failing resource but no terminal
// {"error":true} line must still produce a readable message: the resource
// event carries the same cause in "details". Without that fallback the caller's
// raw-JSON error is left untouched and the user gets the JSON wall.
func TestComposeUpWithProgress_ResourceErrorDetailsAreTheFallback(t *testing.T) {
	lines := fixtureLines(t, "error.jsonl")
	withoutTerminal := strings.Join([]string{string(lines[0]), string(lines[1])}, "\n")
	f := &fakeExec{outConfigYAML: secretDoc, finalStderr: &withoutTerminal, errUp: errors.New("exit status 1")}
	c := &Client{exec: f, identifier: "demo"}
	_, err := c.ComposeUpWithProgress(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", nil, func(ComposeEvent) {})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := apperr.DeepestMessage(err)
	if !strings.Contains(msg, "pull access denied") {
		t.Fatalf("error should carry the resource event's details, got %q", msg)
	}
	if strings.Contains(msg, `{"`) {
		t.Fatalf("error should not show JSON, got %q", msg)
	}
}

// ssh writes "Session open refused by peer" to stderr as PLAIN TEXT, never as a
// compose event, and dockercli.IsSSHSessionLimit finds it through
// apperr.DeepestMessage. planner.Apply uses that to name how many services were
// started concurrently — a diagnostic that disappears if deriving a readable
// compose message discards the undecodable lines. Both must survive.
func TestComposeUpWithProgress_KeepsSSHSessionLimitSignature(t *testing.T) {
	stderr := `{"error":true,"message":"Error response from daemon: cannot start"}` + "\n" +
		"Session open refused by peer\nConnection closed by 10.0.0.1 port 22"
	f := &fakeExec{outConfigYAML: secretDoc, finalStderr: &stderr, errUp: errors.New("exit status 255")}
	c := &Client{exec: f, identifier: "demo"}
	_, err := c.ComposeUpWithProgress(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", nil, func(ComposeEvent) {})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsSSHSessionLimit(err) {
		t.Fatalf("the ssh signature must stay reachable, got %q", apperr.DeepestMessage(err))
	}
	if !strings.Contains(apperr.DeepestMessage(err), "cannot start") {
		t.Fatalf("compose's own message must still lead, got %q", apperr.DeepestMessage(err))
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
	if strings.Contains(apperr.DeepestMessage(err), staleMsg) {
		t.Fatalf("error must not carry an earlier attempt's message, got %q", apperr.DeepestMessage(err))
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
	msg := apperr.DeepestMessage(err)
	if !strings.Contains(msg, plainText) {
		t.Fatalf("error should surface the plain-text stderr, got %q", msg)
	}
	if strings.Contains(msg, `{"`) {
		t.Fatalf("error should not show JSON, got %q", msg)
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
