package dockercli

import (
	"context"
	"errors"
	"testing"
)

// Without the labeled overlay, compose up would create containers without the
// identifier label, which destroy and prune cannot see. It must fail instead.
func TestComposeUp_FailsWhenLabelOverlayCannotBeBuilt(t *testing.T) {
	f := &fakeExec{errConfigYAML: errors.New("ssh: session refused")}
	c := &Client{exec: f, identifier: "demo"}
	if _, err := c.ComposeUp(context.Background(), "/tmp", []string{"a.yml"}, nil, nil, "proj", nil); err == nil {
		t.Fatal("expected an error when the label overlay cannot be built")
	}
	if hasSuffix(f.lastArgs, []string{"up", "-d"}) {
		t.Fatalf("compose up must not run without the overlay; last args %#v", f.lastArgs)
	}
}

// Hashing the unlabeled files yields a hash that never matches the labeled
// containers, so every service would look drifted. It must fail instead.
func TestComposeConfigHash_FailsWhenLabelOverlayCannotBeBuilt(t *testing.T) {
	f := &fakeExec{errConfigYAML: errors.New("ssh: session refused"), outHash: "web abc"}
	c := &Client{exec: f}
	if _, err := c.ComposeConfigHash(context.Background(), ".", nil, nil, nil, "proj", "web", "demo", nil); err == nil {
		t.Fatal("expected an error when the label overlay cannot be built")
	}
	if f.hashCalls != 0 {
		t.Fatalf("compose config --hash must not run without the overlay; ran %d times", f.hashCalls)
	}
}
