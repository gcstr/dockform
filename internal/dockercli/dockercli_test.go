package dockercli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type execStub struct {
	lastArgs    []string
	lastDir     string
	lastEnvUsed bool
	stdinBytes  int

	// canned outputs per verb
	outVersion string
	errVersion error

	outInspect string
	errInspect error

	outPs string
	errPs error
}

func (e *execStub) Run(ctx context.Context, args ...string) (string, error) {
	e.lastArgs = args
	if len(args) > 0 && args[0] == "version" {
		return e.outVersion, e.errVersion
	}
	if len(args) > 0 && args[0] == "inspect" {
		return e.outInspect, e.errInspect
	}
	if len(args) > 0 && args[0] == "ps" {
		return e.outPs, e.errPs
	}
	return "", nil
}

func (e *execStub) RunInDir(ctx context.Context, dir string, args ...string) (string, error) {
	e.lastDir, e.lastArgs, e.lastEnvUsed = dir, args, false
	return e.Run(ctx, args...)
}

func (e *execStub) RunInDirWithEnv(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	e.lastDir, e.lastArgs, e.lastEnvUsed = dir, args, true
	return e.Run(ctx, args...)
}

func (e *execStub) RunWithStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	e.lastArgs = args
	if stdin != nil {
		b, _ := io.ReadAll(stdin)
		e.stdinBytes = len(b)
	}
	return "", nil
}

func TestCheckDaemon_SuccessAndFailure(t *testing.T) {
	stub := &execStub{}
	c := &Client{exec: stub, contextName: "ctx"}
	if err := c.CheckDaemon(context.Background()); err != nil {
		t.Fatalf("check daemon success: %v", err)
	}
	wantErr := errors.New("boom")
	stub.errVersion = wantErr
	if err := c.CheckDaemon(context.Background()); err == nil || !strings.Contains(err.Error(), "docker daemon not reachable (context=ctx)") {
		t.Fatalf("expected context-qualified error, got: %v", err)
	}
}

func TestRemoveContainer_BuildsArgs(t *testing.T) {
	stub := &execStub{}
	c := &Client{exec: stub}
	_ = c.RemoveContainer(context.Background(), "c1", false)
	if !containsArgSeq(stub.lastArgs, []string{"container", "rm", "c1"}) {
		t.Fatalf("args without force mismatch: %#v", stub.lastArgs)
	}
	_ = c.RemoveContainer(context.Background(), "c2", true)
	if !containsArgSeq(stub.lastArgs, []string{"container", "rm", "-f", "c2"}) {
		t.Fatalf("args with force mismatch: %#v", stub.lastArgs)
	}
}

func TestInspectContainerLabels_FilterAndErrors(t *testing.T) {
	stub := &execStub{outInspect: `{"a":"1","b":"2"}`}
	c := &Client{exec: stub}
	if _, err := c.InspectContainerLabels(context.Background(), "", nil); err == nil {
		t.Fatalf("expected error for empty container name")
	}
	m, err := c.InspectContainerLabels(context.Background(), "name", []string{"a"})
	if err != nil || len(m) != 1 || m["a"] != "1" {
		t.Fatalf("filter failed: %v %#v", err, m)
	}
	stub.outInspect = "not json"
	if _, err := c.InspectContainerLabels(context.Background(), "name", nil); err == nil || !strings.Contains(err.Error(), "parse labels json") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestUpdateContainerLabels_BuildsArgs(t *testing.T) {
	stub := &execStub{}
	c := &Client{exec: stub}
	_ = c.UpdateContainerLabels(context.Background(), "name", map[string]string{"k1": "v1", "k2": "v2"})
	if len(stub.lastArgs) == 0 || stub.lastArgs[0] != "container" || stub.lastArgs[1] != "update" {
		t.Fatalf("unexpected args: %#v", stub.lastArgs)
	}
	if !contains(stub.lastArgs, "--label-add") || !contains(stub.lastArgs, "k1=v1") || !contains(stub.lastArgs, "k2=v2") {
		t.Fatalf("missing label args: %#v", stub.lastArgs)
	}
	if stub.lastArgs[len(stub.lastArgs)-1] != "name" {
		t.Fatalf("container name position mismatch: %#v", stub.lastArgs)
	}
}

func TestListComposeContainersAll_ParsesAndFilters(t *testing.T) {
	stub := &execStub{outPs: "proj;web;name1\ninvalid\nproj;;name2\n"}
	c := &Client{exec: stub}
	items, err := c.ListComposeContainersAll(context.Background())
	if err != nil || len(items) != 1 || items[0].Service != "web" {
		t.Fatalf("parse items: %v %#v", err, items)
	}

	c = &Client{exec: stub, identifier: "demo"}
	_, _ = c.ListComposeContainersAll(context.Background())
	joined := strings.Join(stub.lastArgs, " ")
	if !strings.Contains(joined, "--filter label=io.dockform/demo") {
		t.Fatalf("expected identifier filter in args: %s", joined)
	}
}

func TestSyncDirToVolume_ValidatesAndStreamsTar(t *testing.T) {
	stub := &execStub{}
	c := &Client{exec: stub}
	if err := c.SyncDirToVolume(context.Background(), "", "/data", "."); err == nil {
		t.Fatalf("expected error for empty volume name")
	}
	if err := c.SyncDirToVolume(context.Background(), "vol", "data", "."); err == nil {
		t.Fatalf("expected error for non-absolute target path")
	}
	if err := c.SyncDirToVolume(context.Background(), "vol", "/", "."); err == nil {
		t.Fatalf("expected error for root target path")
	}

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "f.txt"), []byte("hello"))
	if err := c.SyncDirToVolume(context.Background(), "vol", "/app", dir); err != nil {
		t.Fatalf("sync dir: %v", err)
	}
	if stub.stdinBytes == 0 {
		t.Fatalf("expected tar stream to be sent to stdin")
	}
	// Validate key bits of args
	joined := strings.Join(stub.lastArgs, " ")
	if !strings.Contains(joined, "run --rm -i") || !strings.Contains(joined, "-v vol:/app") || !strings.Contains(joined, "alpine sh -c") {
		t.Fatalf("unexpected docker run args: %s", joined)
	}
}

func containsArgSeq(args, seq []string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		match := true
		for j := 0; j < len(seq); j++ {
			if args[i+j] != seq[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func mustWriteFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
