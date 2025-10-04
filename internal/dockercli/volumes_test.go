package dockercli

import (
	"bytes"
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
func (v *volExecStub) RunDetailed(ctx context.Context, opts Options, args ...string) (Result, error) {
	out, err := v.Run(ctx, args...)
	return Result{Stdout: out, Stderr: "", ExitCode: 0}, err
}

// ---- Extended tests merged from volumes_more_test.go ----

// scriptExec is a flexible Exec stub with per-method hooks for tests
type scriptExec struct {
	lastArgs           []string
	calls              [][]string
	writtenStdoutBytes int
	readStdinBytes     int

	onRun           func(args []string) (string, error)
	onRunWithStdout func(args []string, w io.Writer) error
	onRunWithStdin  func(args []string, r io.Reader) (string, error)
}

func (s *scriptExec) Run(ctx context.Context, args ...string) (string, error) {
	s.lastArgs = args
	s.calls = append(s.calls, args)
	if s.onRun != nil {
		return s.onRun(args)
	}
	return "", nil
}
func (s *scriptExec) RunInDir(ctx context.Context, dir string, args ...string) (string, error) {
	return s.Run(ctx, args...)
}
func (s *scriptExec) RunInDirWithEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	return s.Run(ctx, args...)
}
func (s *scriptExec) RunWithStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	s.lastArgs = args
	s.calls = append(s.calls, args)
	if stdin != nil {
		b, _ := io.ReadAll(stdin)
		s.readStdinBytes += len(b)
	}
	if s.onRunWithStdin != nil {
		return s.onRunWithStdin(args, stdin)
	}
	return "", nil
}
func (s *scriptExec) RunWithStdout(ctx context.Context, stdout io.Writer, args ...string) error {
	s.lastArgs = args
	s.calls = append(s.calls, args)
	if stdout != nil {
		n, _ := stdout.Write([]byte("STREAM"))
		s.writtenStdoutBytes += n
	}
	if s.onRunWithStdout != nil {
		return s.onRunWithStdout(args, stdout)
	}
	return nil
}
func (s *scriptExec) RunDetailed(ctx context.Context, opts Options, args ...string) (Result, error) {
	out, err := s.Run(ctx, args...)
	return Result{Stdout: out, Stderr: "", ExitCode: 0}, err
}

func TestInspectVolume_ParsesJSON(t *testing.T) {
	stub := &scriptExec{
		onRun: func(args []string) (string, error) {
			if len(args) >= 5 && args[0] == "volume" && args[1] == "inspect" {
				return `{"Driver":"local","Options":{"o":"uid=1000"},"Labels":{"io.dockform.identifier":"demo"}}`, nil
			}
			return "", nil
		},
	}
	c := &Client{exec: stub}
	d, err := c.InspectVolume(context.Background(), "vol")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if d.Driver != "local" || d.Options["o"] != "uid=1000" || d.Labels["io.dockform.identifier"] != "demo" {
		t.Fatalf("unexpected details: %#v", d)
	}
}

func TestStreamTarFromVolume_BuildsArgsAndStreams(t *testing.T) {
	stub := &scriptExec{}
	c := &Client{exec: stub}
	var buf bytes.Buffer
	if err := c.StreamTarFromVolume(context.Background(), "vol", &buf); err != nil {
		t.Fatalf("stream tar: %v", err)
	}
	joined := strings.Join(stub.lastArgs, " ")
	if !strings.Contains(joined, "run --rm") || !strings.Contains(joined, "-v vol:/src:ro") || !strings.Contains(joined, "alpine:3.22 sh -c") {
		t.Fatalf("unexpected args: %s", joined)
	}
	if buf.Len() == 0 {
		t.Fatalf("expected some bytes to be written to stdout writer")
	}
}

func TestStreamTarZstdFromVolume_BuildsArgsAndStreams(t *testing.T) {
	stub := &scriptExec{}
	c := &Client{exec: stub}
	var buf bytes.Buffer
	if err := c.StreamTarZstdFromVolume(context.Background(), "vol", &buf); err != nil {
		t.Fatalf("stream tar.zst: %v", err)
	}
	joined := strings.Join(stub.lastArgs, " ")
	if !strings.Contains(joined, "-v vol:/src:ro") || !strings.Contains(joined, "zstd -q -z") {
		t.Fatalf("unexpected args: %s", joined)
	}
}

func TestIsVolumeEmpty_ParsesOutput(t *testing.T) {
	stub := &scriptExec{onRun: func(args []string) (string, error) { return "empty\n", nil }}
	c := &Client{exec: stub}
	empty, err := c.IsVolumeEmpty(context.Background(), "vol")
	if err != nil || !empty {
		t.Fatalf("expected empty=true: %v %v", empty, err)
	}
	stub.onRun = func(args []string) (string, error) { return "notempty\n", nil }
	empty, err = c.IsVolumeEmpty(context.Background(), "vol")
	if err != nil || empty {
		t.Fatalf("expected empty=false: %v %v", empty, err)
	}
}

func TestClearVolume_RunsRm(t *testing.T) {
	stub := &scriptExec{onRun: func(args []string) (string, error) { return "", nil }}
	c := &Client{exec: stub}
	if err := c.ClearVolume(context.Background(), "vol"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	joined := strings.Join(stub.lastArgs, " ")
	if !strings.Contains(joined, "-v vol:/dst") || !strings.Contains(joined, "rm -rf") {
		t.Fatalf("unexpected args: %s", joined)
	}
}

func TestListContainersUsingVolume_ParsesLines(t *testing.T) {
	stub := &scriptExec{onRun: func(args []string) (string, error) { return "c1\n\nc2\n", nil }}
	c := &Client{exec: stub}
	names, err := c.ListContainersUsingVolume(context.Background(), "vol")
	if err != nil || len(names) != 2 || names[0] != "c1" || names[1] != "c2" {
		t.Fatalf("parse names: %v %#v", err, names)
	}
}

func TestStopContainers_StopsEach(t *testing.T) {
	stub := &scriptExec{onRun: func(args []string) (string, error) { return "", nil }}
	c := &Client{exec: stub}
	if err := c.StopContainers(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(stub.calls) != 2 {
		t.Fatalf("expected 2 stop calls, got %d", len(stub.calls))
	}
	j1 := strings.Join(stub.calls[0], " ")
	j2 := strings.Join(stub.calls[1], " ")
	if !strings.Contains(j1, "container stop a") || !strings.Contains(j2, "container stop b") {
		t.Fatalf("unexpected stop args: %q %q", j1, j2)
	}
}

func TestTarStatsFromVolume_ParsesCounts(t *testing.T) {
	stub := &scriptExec{onRun: func(args []string) (string, error) { return "12 3456\n", nil }}
	c := &Client{exec: stub}
	bytes, files, err := c.TarStatsFromVolume(context.Background(), "vol")
	if err != nil || bytes != 3456 || files != 12 {
		t.Fatalf("unexpected stats: bytes=%d files=%d err=%v", bytes, files, err)
	}
}

func TestExtractZstdTarToVolume_UsesStdin(t *testing.T) {
	stub := &scriptExec{}
	c := &Client{exec: stub}
	in := bytes.NewBufferString("hello")
	if err := c.ExtractZstdTarToVolume(context.Background(), "vol", in); err != nil {
		t.Fatalf("extract zstd: %v", err)
	}
	if stub.readStdinBytes == 0 {
		t.Fatalf("expected stdin to be consumed")
	}
	joined := strings.Join(stub.lastArgs, " ")
	if !strings.Contains(joined, "-v vol:/dst") || !strings.Contains(joined, "zstd -q -d -c | tar -xpf - -C '/dst'") {
		t.Fatalf("unexpected args: %s", joined)
	}
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
