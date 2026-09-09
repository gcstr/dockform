package planner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

// A stack of three services where `compose up` fails the way sshd does when a
// connection's MaxSessions limit is exhausted.
func writeSSHLimitStub(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  volume) [ "$1" = "ls" ] && { echo ""; exit 0; } ;;
  network) [ "$1" = "ls" ] && { echo ""; exit 0; } ;;
  compose)
    saw_config=0; saw_format=0; jsonfmt=0; saw_ps=0; saw_up=0
    for a in "$@"; do
      [ "$a" = "config" ] && saw_config=1
      [ "$a" = "--format" ] && saw_format=1
      [ "$a" = "json" ] && jsonfmt=1
      [ "$a" = "ps" ] && saw_ps=1
      [ "$a" = "up" ] && saw_up=1
    done
    if [ $saw_up -eq 1 ]; then
      echo "mux_client_request_session: session request failed: Session open refused by peer" 1>&2
      echo "Connection closed by 10.0.0.1 port 22" 1>&2
      exit 1
    fi
    if [ $saw_config -eq 1 ] && [ $saw_format -eq 1 ] && [ $jsonfmt -eq 1 ]; then
      echo '{"services":{"a":{},"b":{},"c":{}}}'; exit 0
    fi
    if [ $saw_ps -eq 1 ] && [ $saw_format -eq 1 ] && [ $jsonfmt -eq 1 ]; then echo "[]"; exit 0; fi
    exit 0 ;;
  inspect) echo '{}'; exit 0 ;;
  ps) echo ""; exit 0 ;;
esac
exit 0
`
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	old := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	t.Cleanup(func() { _ = os.Setenv("PATH", old) })
}

// When compose up fails because sshd refused a session, the error must say how
// many services were being started — that count is what overflowed the limit,
// and without it the user has to go and count their stack by hand.
func TestApply_SSHSessionLimit_ErrorNamesServiceCount(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows due to shell script compatibility")
	}
	writeSSHLimitStub(t)

	cfg := manifest.Config{
		Identifier: "demo",
		Contexts:   map[string]manifest.ContextConfig{"default": {}},
		Stacks:     map[string]manifest.Stack{"default/app": {Root: t.TempDir(), Files: []string{"compose.yml"}}},
	}
	d := dockercli.New("").WithIdentifier("demo")
	err := NewWithDocker(d).Apply(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected compose up to fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "3 services") {
		t.Errorf("error should name the concurrent service count; got: %v", msg)
	}
	if !strings.Contains(msg, "default/app") {
		t.Errorf("error should still name the stack; got: %v", msg)
	}
}

// The limit can also be hit on the label-verification step that runs after a
// successful `compose up`. In live reproduction against a real host that is the
// site that fails most often, so it must carry the same context as the up path.
func TestApply_SSHSessionLimit_OnComposePs_NamesServiceCount(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows due to shell script compatibility")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"cmd=\"$1\"; shift\n" +
		"case \"$cmd\" in\n" +
		"  volume) [ \"$1\" = \"ls\" ] && { echo \"\"; exit 0; } ;;\n" +
		"  network) [ \"$1\" = \"ls\" ] && { echo \"\"; exit 0; } ;;\n" +
		"  compose)\n" +
		"    for a in \"$@\"; do [ \"$a\" = \"config\" ] && sc=1; [ \"$a\" = \"json\" ] && sj=1; [ \"$a\" = \"ps\" ] && sp=1; done\n" +
		"    if [ \"$sc\" = \"1\" ] && [ \"$sj\" = \"1\" ]; then echo '{\"services\":{\"a\":{},\"b\":{},\"c\":{}}}'; exit 0; fi\n" +
		"    if [ \"$sp\" = \"1\" ]; then echo \"mux_client_request_session: session request failed: Session open refused by peer\" 1>&2; exit 1; fi\n" +
		"    exit 0 ;;\n" +
		"  inspect) echo '{}'; exit 0 ;;\n" +
		"  ps) echo \"\"; exit 0 ;;\n" +
		"esac\n" +
		"exit 0\n"
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	old := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	t.Cleanup(func() { _ = os.Setenv("PATH", old) })

	cfg := manifest.Config{
		Identifier: "demo",
		Contexts:   map[string]manifest.ContextConfig{"default": {}},
		Stacks:     map[string]manifest.Stack{"default/app": {Root: t.TempDir(), Files: []string{"compose.yml"}}},
	}
	err := NewWithDocker(dockercli.New("").WithIdentifier("demo")).Apply(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected the compose ps step to fail")
	}
	if !strings.Contains(err.Error(), "3 services") {
		t.Errorf("error should name the concurrent service count; got: %v", err)
	}
}
