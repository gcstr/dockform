package dockercli

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
)

// A resolved document as `compose config` emits it: name: is always present.
const netbirdDoc = "name: netbird\nservices:\n  app:\n    image: alpine:3.22\n"

// The stack directory's basename deliberately differs from the project name,
// which is the case where a moved project directory could rename the project.
const netbirdRoot = "/Users/me/homelab/hetzner-two/netbird-stack"

// flagValue returns the argument following flag, and whether flag was present.
func flagValue(args []string, flag string) (string, bool) {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

// callEndingIn returns the first recorded call whose args end with suffix.
func callEndingIn(t *testing.T, f *fakeExec, suffix ...string) []string {
	t.Helper()
	for _, c := range f.calls {
		if hasSuffix(c, suffix) {
			return c
		}
	}
	t.Fatalf("no call ending in %v among %v", suffix, f.calls)
	return nil
}

// assertStamped checks the up call carries the synthetic project directory and
// that the call rendering the document does not.
func assertStamped(t *testing.T, f *fakeExec) {
	t.Helper()
	up := callEndingIn(t, f, "up", "-d")
	got, ok := flagValue(up, "--project-directory")
	if !ok || got != "/dockform/homeserver/netbird" {
		t.Fatalf("up must pass --project-directory /dockform/homeserver/netbird; got %q (present=%v) in %v", got, ok, up)
	}
	// Were name: ever missing from the document, compose would name the project
	// after this directory's basename; it must be the document's own project.
	if base := filepath.Base(got); base != "netbird" {
		t.Errorf("basename %q must equal the project name netbird", base)
	}
	// Moving the project directory stops compose auto-loading the stack's .env,
	// so the call that renders (interpolates) the document must keep the real one.
	if render := callEndingIn(t, f, "config"); slices.Contains(render, "--project-directory") {
		t.Errorf("the rendering config call must not move the project directory; got %v", render)
	}
}

func TestComposeUp_StampsASyntheticProjectDirectory(t *testing.T) {
	f := &fakeExec{outConfigYAML: netbirdDoc}
	c := &Client{exec: f, identifier: "homeserver"}
	if _, err := c.ComposeUp(context.Background(), netbirdRoot, []string{"compose.yaml"}, nil, nil, "", nil); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	assertStamped(t, f)
}

func TestComposeUpWithProgress_StampsASyntheticProjectDirectory(t *testing.T) {
	f := &fakeExec{outConfigYAML: netbirdDoc}
	c := &Client{exec: f, identifier: "homeserver"}
	if _, err := c.ComposeUpWithProgress(context.Background(), netbirdRoot, []string{"compose.yaml"}, nil, nil, "", nil, func(ComposeEvent) {}); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	assertStamped(t, f)
}

// Without an identifier nothing is piped: compose reads the user's own files,
// so it needs the real project directory for .env and relative paths.
func TestComposeUp_NoIdentifierKeepsTheRealProjectDirectory(t *testing.T) {
	f := &fakeExec{}
	c := &Client{exec: f}
	if _, err := c.ComposeUp(context.Background(), netbirdRoot, []string{"compose.yaml"}, nil, nil, "", nil); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	if up := callEndingIn(t, f, "up", "-d"); slices.Contains(up, "--project-directory") {
		t.Errorf("no identifier means no piped document, so no synthetic directory; got %v", up)
	}
}

// A document without name: gives no project to name the directory after.
// Inventing one could rename the project, so compose's default is kept.
func TestComposeUp_NoProjectNameKeepsTheRealProjectDirectory(t *testing.T) {
	f := &fakeExec{outConfigYAML: "services:\n  app:\n    image: alpine:3.22\n"}
	c := &Client{exec: f, identifier: "homeserver"}
	if _, err := c.ComposeUp(context.Background(), netbirdRoot, []string{"compose.yaml"}, nil, nil, "", nil); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	if up := callEndingIn(t, f, "up", "-d"); slices.Contains(up, "--project-directory") {
		t.Errorf("no name: in the document means no synthetic directory; got %v", up)
	}
}

// Hashing must see the same project as plan does; only up is stamped.
func TestComposeConfigHash_KeepsTheRealProjectDirectory(t *testing.T) {
	f := &fakeExec{outConfigYAML: netbirdDoc, outHash: "app abc123\n"}
	c := &Client{exec: f, identifier: "homeserver"}
	if _, err := c.ComposeConfigHash(context.Background(), netbirdRoot, []string{"compose.yaml"}, nil, nil, "", "app", "homeserver", nil); err != nil {
		t.Fatalf("hash: %v", err)
	}
	for _, call := range f.calls {
		if slices.Contains(call, "--project-directory") {
			t.Errorf("no hashing call may move the project directory; got %v", call)
		}
	}
}
