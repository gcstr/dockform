package dockercli

import (
	"context"
	"io"
	"strings"
	"testing"
)

type volExecStub struct{ lastArgs []string }

func (v *volExecStub) Run(ctx context.Context, args ...string) (string, error) {
	v.lastArgs = args
	if len(args) >= 2 && args[0] == "volume" && args[1] == "ls" {
		return "vol1\n\nvol2\n", nil
	}
	return "", nil
}
func (v *volExecStub) RunInDir(ctx context.Context, dir string, args ...string) (string, error) {
	return v.Run(ctx, args...)
}
func (v *volExecStub) RunInDirWithEnv(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	return v.Run(ctx, args...)
}
func (v *volExecStub) RunWithStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	v.lastArgs = args
	return "", nil
}

func (v *volExecStub) RunWithStdout(ctx context.Context, stdout io.Writer, args ...string) error {
	v.lastArgs = args
	return nil
}

func TestListVolumes_ParsesAndFilters(t *testing.T) {
	stub := &volExecStub{}
	c := &Client{exec: stub}
	vols, err := c.ListVolumes(context.Background())
	if err != nil || len(vols) != 2 || vols[0] != "vol1" || vols[1] != "vol2" {
		t.Fatalf("list volumes parse: %v %#v", err, vols)
	}

	// With identifier, ensure filter flag is present
	c = &Client{exec: stub, identifier: "demo"}
	_, _ = c.ListVolumes(context.Background())
	joined := strings.Join(stub.lastArgs, " ")
	if !strings.Contains(joined, "--filter label=io.dockform.identifier=demo") {
		t.Fatalf("expected identifier filter in args: %s", joined)
	}
}

func TestCreateVolume_AddsLabels(t *testing.T) {
	stub := &volExecStub{}
	c := &Client{exec: stub}
	if err := c.CreateVolume(context.Background(), "v1", map[string]string{"a": "1", "b": "2"}); err != nil {
		t.Fatalf("create volume: %v", err)
	}
	if len(stub.lastArgs) == 0 || stub.lastArgs[0] != "volume" || stub.lastArgs[1] != "create" {
		t.Fatalf("unexpected args: %#v", stub.lastArgs)
	}
	if !contains(stub.lastArgs, "--label") || !contains(stub.lastArgs, "a=1") || !contains(stub.lastArgs, "b=2") {
		t.Fatalf("missing label args: %#v", stub.lastArgs)
	}
	if stub.lastArgs[len(stub.lastArgs)-1] != "v1" {
		t.Fatalf("volume name position mismatch: %#v", stub.lastArgs)
	}
}

func TestRemoveVolume_Args(t *testing.T) {
	stub := &volExecStub{}
	c := &Client{exec: stub}
	if err := c.RemoveVolume(context.Background(), "v1"); err != nil {
		t.Fatalf("remove volume: %v", err)
	}
	if !containsArgSeq(stub.lastArgs, []string{"volume", "rm", "v1"}) {
		t.Fatalf("unexpected args: %#v", stub.lastArgs)
	}
}
