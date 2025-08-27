package dockercli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeDockerExecStub(t *testing.T, dir string) string {
	t.Helper()
	script := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "CTX=$DOCKER_CONTEXT"
    exit 0 ;;
  pwdcmd)
    pwd; exit 0 ;;
  envcmd)
    echo "PWD=$(pwd) FOO=$FOO"; exit 0 ;;
  stdin)
    cat -; exit 0 ;;
  fail)
    echo "FAIL OUT"
    echo "failure details from docker" 1>&2
    exit 2 ;;
esac
exit 0
`
	path := filepath.Join(dir, "docker")
	if runtime.GOOS == "windows" {
		path += ".cmd"
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

func withDockerExecStub(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	_ = writeDockerExecStub(t, dir)
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return func() { _ = os.Setenv("PATH", oldPath) }
}

func TestSystemExec_Run_SetsContextAndCapturesStdout(t *testing.T) {
	defer withDockerExecStub(t)()
	s := SystemExec{ContextName: "myctx"}
	out, err := s.Run(context.Background(), "version")
	if err != nil {
		t.Fatalf("run version: %v", err)
	}
	if out == "" || out[:4] != "CTX=" {
		t.Fatalf("unexpected stdout: %q", out)
	}
	if want := "CTX=myctx"; out[:len(want)] != want {
		t.Fatalf("expected DOCKER_CONTEXT propagated, got %q", out)
	}
}

func TestSystemExec_RunInDir_And_WithEnv(t *testing.T) {
	defer withDockerExecStub(t)()
	s := SystemExec{}
	wd := t.TempDir()
	out, err := s.RunInDir(context.Background(), wd, "pwdcmd")
	if err != nil {
		t.Fatalf("run in dir: %v", err)
	}
	outTrim := strings.TrimSpace(out)
	wantResolved, _ := filepath.EvalSymlinks(wd)
	gotResolved, _ := filepath.EvalSymlinks(outTrim)
	if wantResolved != gotResolved {
		t.Fatalf("expected PWD to be %q, got %q", wantResolved, gotResolved)
	}
	// With extra env
	out, err = s.RunInDirWithEnv(context.Background(), wd, []string{"FOO=bar"}, "envcmd")
	if err != nil {
		t.Fatalf("run in dir with env: %v", err)
	}
	if !stringContains(out, "FOO=bar") {
		t.Fatalf("expected output to contain FOO=bar; got %q", out)
	}
}

func stringContains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

func TestSystemExec_RunWithStdin_ForwardsInput(t *testing.T) {
	defer withDockerExecStub(t)()
	s := SystemExec{}
	in := bytes.NewBufferString("hello world\n")
	out, err := s.RunWithStdin(context.Background(), in, "stdin")
	if err != nil {
		t.Fatalf("run with stdin: %v", err)
	}
	if out != "hello world\n" {
		t.Fatalf("unexpected stdout from cat, got %q", out)
	}
}

func TestSystemExec_Run_ErrorWrapsStderr(t *testing.T) {
	defer withDockerExecStub(t)()
	s := SystemExec{}
	out, err := s.Run(context.Background(), "fail")
	if err == nil {
		t.Fatalf("expected error from fail script")
	}
	if !stringContains(err.Error(), "failure details from docker") {
		t.Fatalf("expected stderr content in error: %v", err)
	}
	if out == "" || !stringContains(out, "FAIL OUT") {
		t.Fatalf("expected stdout to be returned even on error; got %q", out)
	}
}
