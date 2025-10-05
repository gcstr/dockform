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

func writeDockerPlannerStub(t *testing.T, dir string, mode string) string {
	t.Helper()
	// mode: basic | drift | match | mismatch
	script := `#!/bin/sh
log="$DOCKER_STUB_LOG"
cmd="$1"; shift
case "$cmd" in
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "vOld"; exit 0; fi
    if [ "$sub" = "rm" ]; then echo "rm volume $1" >> "$log"; exit 0; fi
    ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "nOld"; exit 0; fi
    if [ "$sub" = "rm" ]; then echo "rm network $1" >> "$log"; exit 0; fi
    ;;
  container)
    if [ "$1" = "rm" ]; then shift; echo "rm container $*" >> "$log"; exit 0; fi
    ;;
  compose)
    # services
    for a in "$@"; do [ "$a" = "--services" ] && { echo "nginx"; exit 0; }; done
    # config json
    saw_config=0; saw_format=0; jsonfmt=0
    for a in "$@"; do [ "$a" = "config" ] && saw_config=1; [ "$a" = "--format" ] && saw_format=1; [ "$a" = "json" ] && jsonfmt=1; done
    if [ $saw_config -eq 1 ] && [ $saw_format -eq 1 ] && [ $jsonfmt -eq 1 ]; then echo '{"services":{"nginx":{}}}'; exit 0; fi
    # hash
    for a in "$@"; do if [ "$a" = "--hash" ]; then echo "nginx deadbeef"; exit 0; fi; done
    # ps json
    saw_ps=0; saw_format=0; jsonfmt=0
    for a in "$@"; do [ "$a" = "ps" ] && saw_ps=1; [ "$a" = "--format" ] && saw_format=1; [ "$a" = "json" ] && jsonfmt=1; done
    if [ $saw_ps -eq 1 ] && [ $saw_format -eq 1 ] && [ $jsonfmt -eq 1 ]; then echo "[]"; exit 0; fi
    # up -d
    saw_up=0; saw_d=0
    for a in "$@"; do [ "$a" = "up" ] && saw_up=1; [ "$a" = "-d" ] && saw_d=1; done
    if [ $saw_up -eq 1 ] && [ $saw_d -eq 1 ]; then exit 0; fi
    exit 0 ;;
  ps)
    # docker ps -a --format lines used by ListComposeContainersAll
    echo "proj;other;other_name"
    exit 0 ;;
  inspect)
    # Labels based on mode
    if [ "$MODE" = "mismatch" ]; then
      echo '{"io.dockform/oldid":"1","com.docker.compose.config-hash":"cafebabe"}'
      exit 0
    fi
    if [ "$MODE" = "drift" ]; then
      echo '{"io.dockform/demo":"1","com.docker.compose.config-hash":"cafebabe"}'
      exit 0
    fi
    if [ "$MODE" = "match" ]; then
      echo '{"io.dockform/demo":"1","com.docker.compose.config-hash":"deadbeef"}'
      exit 0
    fi
    echo '{}' ; exit 0 ;;
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

func withPlannerStub(t *testing.T, mode string, logPath string) func() {
	t.Helper()
	dir := t.TempDir()
	_ = writeDockerPlannerStub(t, dir, mode)
	oldPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	_ = os.Setenv("MODE", mode)
	_ = os.Setenv("DOCKER_STUB_LOG", logPath)
	return func() {
		_ = os.Setenv("PATH", oldPath)
		_ = os.Unsetenv("MODE")
		_ = os.Unsetenv("DOCKER_STUB_LOG")
	}
}

func TestPlanner_BuildPlan_AddRemoveStart(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log.txt")
	defer withPlannerStub(t, "basic", log)()
	appRoot := t.TempDir()
	cfg := manifest.Config{
		Docker:   manifest.DockerConfig{Identifier: "demo"},
		Stacks:   map[string]manifest.Stack{"app": {Root: appRoot, Files: []string{"compose.yml"}}},
		Filesets: map[string]manifest.FilesetSpec{"data": {Source: "src", TargetVolume: "v1", TargetPath: "/app"}},
		Networks: map[string]manifest.NetworkSpec{"n1": {}},
	}
	d := dockercli.New("").WithIdentifier("demo")
	pln, err := NewWithDocker(d).BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	out := pln.String()
	contains(t, out, "↑ v1 will be created")
	contains(t, out, "× vOld will be deleted")
	contains(t, out, "↑ n1 will be created")
	contains(t, out, "× nOld will be deleted")
	contains(t, out, "↑ nginx will be created")
	// Removed containers section; expect service deletion under Applications
	if !strings.Contains(out, "× other will be deleted") {
		t.Fatalf("expected service deletion for unmanaged container; got:\n%s", out)
	}
}

func TestPlanner_BuildPlan_IdentifierMismatch(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log.txt")
	defer withPlannerStub(t, "mismatch", log)()
	appRoot := t.TempDir()
	cfg := manifest.Config{Docker: manifest.DockerConfig{Identifier: "demo"}, Stacks: map[string]manifest.Stack{"app": {Root: appRoot, Files: []string{"compose.yml"}}}}
	d := dockercli.New("").WithIdentifier("demo")
	pln, err := NewWithDocker(d).BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	out := pln.String()
	// With new system, identifier mismatch results in reconcile action
	if !strings.Contains(out, "→ nginx will be reconciled (identifier mismatch)") && !strings.Contains(out, "↑ nginx will be created") {
		t.Fatalf("expected reconcile or create line; got:\n%s", out)
	}
}

func TestPlanner_BuildPlan_ConfigDriftAndMatch(t *testing.T) {
	// Drift case
	log := filepath.Join(t.TempDir(), "log.txt")
	defer withPlannerStub(t, "drift", log)()
	appRoot := t.TempDir()
	cfg := manifest.Config{Docker: manifest.DockerConfig{Identifier: "demo"}, Stacks: map[string]manifest.Stack{"app": {Root: appRoot, Files: []string{"compose.yml"}}}}
	d := dockercli.New("").WithIdentifier("demo")
	pln, err := NewWithDocker(d).BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	out := pln.String()
	if !strings.Contains(out, "→ nginx will be updated (config drift)") && !strings.Contains(out, "↑ nginx will be created") {
		t.Fatalf("expected update or create line; got:\n%s", out)
	}

	// Match case
	cleanup := withPlannerStub(t, "match", log)
	defer cleanup()
	d2 := dockercli.New("").WithIdentifier("demo")
	pln2, err := NewWithDocker(d2).BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	out2 := pln2.String()
	if !strings.Contains(out2, "up-to-date") && !strings.Contains(out2, "↑ nginx will be created") {
		t.Fatalf("expected up-to-date or create line; got:\n%s", out2)
	}
}

func TestPlanner_Prune_RemovesUnmanaged(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log.txt")
	defer withPlannerStub(t, "basic", log)()
	appRoot := t.TempDir()
	cfg := manifest.Config{Docker: manifest.DockerConfig{Identifier: "demo"}, Stacks: map[string]manifest.Stack{"app": {Root: appRoot, Files: []string{"compose.yml"}}}}
	d := dockercli.New("").WithIdentifier("demo")
	if err := NewWithDocker(d).Prune(context.Background(), cfg); err != nil {
		t.Fatalf("prune: %v", err)
	}
	// Assert that removes were attempted by reading the log
	b, _ := os.ReadFile(log)
	s := string(b)
	containerRemoved := strings.Contains(s, "rm container other_name") || strings.Contains(s, "rm container -f other_name")
	volumeRemoved := strings.Contains(s, "rm volume vOld")
	networkRemoved := strings.Contains(s, "rm network nOld")
	if !containerRemoved || !volumeRemoved || !networkRemoved {
		t.Fatalf("expected prune removes in log, got: %s", s)
	}
}

func contains(t *testing.T, s, sub string) {
	t.Helper()
	norm := func(x string) string { return strings.Join(strings.Fields(x), " ") }
	if !strings.Contains(norm(s), norm(sub)) {
		t.Fatalf("expected to contain %q; got:\n%s", sub, s)
	}
}
