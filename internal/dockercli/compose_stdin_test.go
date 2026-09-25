package dockercli

import (
	"context"
	"strings"
	"testing"
)

// The labeled compose document holds SOPS-decrypted values (compose config
// interpolates them), so it is piped to compose on stdin as its only compose
// file and never written to disk.

const secretDoc = "services:\n  web:\n    image: nginx\n    environment:\n      SECRET: s3cr3t\n"

func assertPipedDocument(t *testing.T, f *fakeExec) {
	t.Helper()
	joined := strings.Join(f.lastArgs, " ")
	if strings.Count(joined, "-f ") != 1 || !strings.Contains(joined, "-f - ") {
		t.Fatalf("expected a single -f - (stdin), got args: %s", joined)
	}
	if !strings.Contains(string(f.lastStdin), "io.dockform.identifier: demo") {
		t.Fatalf("expected the labeled document on stdin, got %q", f.lastStdin)
	}
}

func TestComposeUp_PipesLabeledDocumentOverStdin(t *testing.T) {
	f := &fakeExec{outConfigYAML: secretDoc}
	c := &Client{exec: f, identifier: "demo"}
	if _, err := c.ComposeUp(context.Background(), t.TempDir(), []string{"a.yml", "b.yml"}, nil, nil, "proj", []string{"SECRET=s3cr3t"}); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	if !hasSuffix(f.lastArgs, []string{"up", "-d"}) {
		t.Fatalf("expected up -d, got %#v", f.lastArgs)
	}
	assertPipedDocument(t, f)
	if !f.lastWithEnv {
		t.Fatal("inline env must still reach compose up")
	}
}

func TestComposeConfigHash_PipesLabeledDocumentOverStdin(t *testing.T) {
	f := &fakeExec{outConfigYAML: secretDoc, outHash: "web abc"}
	c := &Client{exec: f}
	if _, err := c.ComposeConfigHash(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", "web", "demo", nil); err != nil {
		t.Fatalf("config hash: %v", err)
	}
	assertPipedDocument(t, f)
}

func TestComposeConfigHashes_PipesLabeledDocumentOverStdin(t *testing.T) {
	f := &fakeExec{outConfigYAML: secretDoc, outHash: "web abc\n"}
	c := &Client{exec: f}
	if _, err := c.ComposeConfigHashes(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", []string{"web"}, "demo", nil); err != nil {
		t.Fatalf("config hashes: %v", err)
	}
	assertPipedDocument(t, f)
}

func TestComposeUpServices_ScopesToServicesWithoutDeps(t *testing.T) {
	f := &fakeExec{outConfigYAML: secretDoc}
	c := &Client{exec: f, identifier: "demo"}
	if _, err := c.ComposeUpServices(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", []string{"web", "cache"}, []string{"SECRET=s3cr3t"}); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	if !hasSuffix(f.lastArgs, []string{"up", "-d", "--no-deps", "web", "cache"}) {
		t.Fatalf("expected up -d --no-deps web cache, got %#v", f.lastArgs)
	}
	// Same labeled, secret-carrying document as a full compose up.
	assertPipedDocument(t, f)
	if !f.lastWithEnv {
		t.Fatal("inline env must still reach compose up")
	}
}

func TestComposeUpServices_RefusesEmptyServiceList(t *testing.T) {
	f := &fakeExec{outConfigYAML: secretDoc}
	c := &Client{exec: f, identifier: "demo"}
	if _, err := c.ComposeUpServices(context.Background(), t.TempDir(), []string{"a.yml"}, nil, nil, "proj", nil, nil); err == nil {
		t.Fatal("expected an error: an empty service list would bring up the whole stack")
	}
	if len(f.calls) != 0 {
		t.Fatalf("nothing should run for an empty service list, got %#v", f.calls)
	}
}
