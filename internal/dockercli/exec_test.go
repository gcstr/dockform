package dockercli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeDockerExecStub(t *testing.T, dir string) string {
	t.Helper()
	var script string
	path := filepath.Join(dir, "docker")

	if runtime.GOOS == "windows" {
		// Windows batch script
		path += ".cmd"
		script = `@echo off
if "%1"=="version" (
  echo CTX=%DOCKER_CONTEXT%
  exit /b 0
)
if "%1"=="pwdcmd" (
  cd
  exit /b 0
)
if "%1"=="envcmd" (
  echo PWD=%CD% FOO=%FOO%
  exit /b 0
)
if "%1"=="hostcmd" (
  echo HOST=%DOCKER_HOST% CTX=%DOCKER_CONTEXT%
  exit /b 0
)
if "%1"=="stdin" (
  more
  exit /b 0
)
if "%1"=="fail" (
  echo FAIL OUT
  echo failure details from docker 1>&2
  exit /b 2
)
exit /b 0
`
	} else {
		// Unix shell script
		script = `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "CTX=$DOCKER_CONTEXT"
    exit 0 ;;
  pwdcmd)
    pwd; exit 0 ;;
  envcmd)
    echo "PWD=$(pwd) FOO=$FOO"; exit 0 ;;
  hostcmd)
    echo "HOST=$DOCKER_HOST CTX=$DOCKER_CONTEXT"; exit 0 ;;
  stdin)
    cat -; exit 0 ;;
  fail)
    echo "FAIL OUT"
    echo "failure details from docker" 1>&2
    exit 2 ;;
esac
exit 0
`
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

func TestSystemExec_Run_HostOverrideSetsDockerHostAndSuppressesContext(t *testing.T) {
	defer withDockerExecStub(t)()
	s := SystemExec{ContextName: "myctx", HostOverride: "ssh://user@server"}
	out, err := s.Run(context.Background(), "hostcmd")
	if err != nil {
		t.Fatalf("run hostcmd: %v", err)
	}
	if !stringContains(out, "HOST=ssh://user@server") {
		t.Fatalf("expected DOCKER_HOST to be set, got %q", out)
	}
	if stringContains(out, "CTX=myctx") {
		t.Fatalf("expected DOCKER_CONTEXT to NOT be set when HostOverride is used, got %q", out)
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
	// On Windows, 'more' adds CRLF line endings; normalize for comparison
	want := "hello world\n"
	if runtime.GOOS == "windows" {
		// Windows 'more' may add extra line endings
		out = strings.ReplaceAll(out, "\r\n", "\n")
		out = strings.TrimSpace(out) + "\n"
	}
	if out != want {
		t.Fatalf("unexpected stdout, got %q want %q", out, want)
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

// writeCountingFailStub writes a `docker` stub that appends one line to
// counterPath on every invocation and exits non-zero, so tests can count attempts.
func writeCountingFailStub(t *testing.T, dir, counterPath string) {
	t.Helper()
	// Emit an SSH-connection-error on stderr so the retry path (which only fires
	// for those errors) is exercised for the non-probe baseline.
	script := "#!/bin/sh\n" +
		"echo x >> '" + counterPath + "'\n" +
		"echo 'kex_exchange_identification: Connection reset by peer' 1>&2\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return 0 // file not created => zero invocations
	}
	return strings.Count(string(b), "x\n")
}

func TestRunDetailed_Probe_SkipsRetry(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "calls.txt")
	writeCountingFailStub(t, dir, counter)
	oldPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	defer func() { _ = os.Setenv("PATH", oldPath) }()

	// Shrink the backoff so the non-probe baseline doesn't sleep ~15s.
	oldDelay := sshRetryBaseDelay
	sshRetryBaseDelay = time.Millisecond
	defer func() { sshRetryBaseDelay = oldDelay }()

	s := SystemExec{sem: make(chan struct{}, MaxConcurrentSSH)}

	// Probe: exactly one attempt, no retries.
	_, err := s.RunDetailed(context.Background(), Options{Probe: true}, "fail")
	if err == nil {
		t.Fatal("expected error from failing stub")
	}
	if n := countLines(t, counter); n != 1 {
		t.Fatalf("probe call: expected 1 docker invocation, got %d", n)
	}

	// Non-probe baseline: 1 + sshMaxRetries attempts (proves retry still active by default).
	_ = os.Remove(counter)
	_, _ = s.RunDetailed(context.Background(), Options{}, "fail")
	if n := countLines(t, counter); n != sshMaxRetries+1 {
		t.Fatalf("non-probe call: expected %d invocations, got %d", sshMaxRetries+1, n)
	}
}

func TestRunDetailed_Probe_SkipsSemaphore(t *testing.T) {
	defer withDockerExecStub(t)() // provides a `docker version` stub that exits 0
	s := SystemExec{sem: make(chan struct{}, 1)}
	s.sem <- struct{}{} // saturate the semaphore: a non-probe call would block

	// Probe call must proceed despite the full semaphore.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := s.RunDetailed(ctx, Options{Probe: true}, "version"); err != nil {
		t.Fatalf("probe call should bypass full semaphore, got: %v", err)
	}

	// Control: a non-probe call blocks on the full semaphore until ctx expires.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel2()
	if _, err := s.RunDetailed(ctx2, Options{}, "version"); err == nil {
		t.Fatal("non-probe call should block on full semaphore and hit ctx deadline")
	}
}
