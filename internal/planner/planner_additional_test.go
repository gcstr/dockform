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
	"github.com/gcstr/dockform/internal/ui"
)

func writeComposeErrorStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  compose)
    for a in "$@"; do [ "$a" = "config" ] && { echo "boom" 1>&2; exit 1; }; done ;;
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
	old := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	t.Cleanup(func() { _ = os.Setenv("PATH", old) })
	return path
}

func TestBuildPlan_NoDocker_ClientNil(t *testing.T) {
	cfg := manifest.Config{}
	pln, err := New().BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	out := pln.String()
	if !strings.Contains(out, "no applications defined") && out == "" {
		t.Fatalf("expected a no-op line or empty output; got:\n%s", out)
	}
}

func TestBuildPlan_NoDocker_AppsPlannedTBD(t *testing.T) {
	cfg := manifest.Config{Applications: map[string]manifest.Application{"app": {Root: t.TempDir(), Files: []string{"compose.yml"}}}}
	pln, err := New().BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	out := pln.String()
	if !strings.Contains(out, "application app planned (services diff TBD)") {
		t.Fatalf("expected planned TBD line; got:\n%s", out)
	}
}

func TestBuildPlan_ComposeConfigError(t *testing.T) {
	_ = writeComposeErrorStub(t)
	cfg := manifest.Config{Applications: map[string]manifest.Application{"app": {Root: t.TempDir(), Files: []string{"compose.yml"}}}}
	d := dockercli.New("")
	pln, err := NewWithDocker(d).BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildPlan should not fail, got: %v", err)
	}
	// With the new ServiceStateDetector, compose config errors result in fallback "TBD" messages instead of hard errors
	out := pln.String()
	if !strings.Contains(out, "application app planned (services diff TBD)") {
		t.Fatalf("expected TBD fallback for compose config error, got:\n%s", out)
	}
}

func TestApply_Precondition_NoDocker(t *testing.T) {
	cfg := manifest.Config{}
	err := New().Apply(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "docker client not configured") {
		t.Fatalf("expected precondition error, got: %v", err)
	}
}

func TestApply_ComposeConfigError(t *testing.T) {
	_ = writeComposeErrorStub(t)
	cfg := manifest.Config{Applications: map[string]manifest.Application{"app": {Root: t.TempDir(), Files: []string{"compose.yml"}}}}
	d := dockercli.New("")
	err := NewWithDocker(d).Apply(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "failed to detect service states") {
		t.Fatalf("expected service detection error, got: %v", err)
	}
}

func TestApply_NoChanges_NoComposeUp(t *testing.T) {
	// Stub that reports planned service and running container with matching labels and hash
	dir := t.TempDir()
	script := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  volume)
    if [ "$1" = "ls" ]; then echo ""; exit 0; fi ;;
  network)
    if [ "$1" = "ls" ]; then echo ""; exit 0; fi ;;
  compose)
    # config json
    for a in "$@"; do [ "$a" = "config" ] && saw_config=1; [ "$a" = "--format" ] && saw_format=1; [ "$a" = "json" ] && jsonfmt=1; done
    if [ "$saw_config" = "1" ] && [ "$saw_format" = "1" ] && [ "$jsonfmt" = "1" ]; then echo '{"services":{"nginx":{}}}'; exit 0; fi
    # ps -> one item
    for a in "$@"; do [ "$a" = "ps" ] && saw_ps=1; [ "$a" = "--format" ] && saw_format=1; [ "$a" = "json" ] && jsonfmt=1; done
    if [ "$saw_ps" = "1" ] && [ "$saw_format" = "1" ] && [ "$jsonfmt" = "1" ]; then echo '[{"Name":"c1","Service":"nginx"}]'; exit 0; fi
    # up should not be called; if it is, write marker file
    for a in "$@"; do [ "$a" = "up" ] && echo up_called > "$DOCKER_STUB_LOG"; done
    exit 0 ;;
  inspect)
    echo '{"io.dockform.identifier":"demo","com.docker.compose.config-hash":"deadbeef"}'
    exit 0 ;;
  ps)
    echo "proj;nginx;name"
    exit 0 ;;
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
	old := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	t.Cleanup(func() { _ = os.Setenv("PATH", old) })

	cfg := manifest.Config{Docker: manifest.DockerConfig{Identifier: "demo"}, Applications: map[string]manifest.Application{
		"app": {Root: t.TempDir(), Files: []string{"compose.yml"}},
	}}
	d := dockercli.New("").WithIdentifier("demo")
	log := filepath.Join(t.TempDir(), "log.txt")
	if err := os.Setenv("DOCKER_STUB_LOG", log); err != nil {
		t.Fatalf("setenv DOCKER_STUB_LOG: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("DOCKER_STUB_LOG"); err != nil {
			t.Logf("unsetenv DOCKER_STUB_LOG: %v", err)
		}
	}()
	if err := NewWithDocker(d).Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Assert that 'up' was not called
	b, _ := os.ReadFile(log)
	if strings.Contains(string(b), "up_called") {
		t.Fatalf("compose up should not be called when no changes needed")
	}
}

func TestPrune_Precondition_NoDocker(t *testing.T) {
	cfg := manifest.Config{}
	err := New().Prune(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "docker client not configured") {
		t.Fatalf("expected precondition error, got: %v", err)
	}
}

func TestPlanString_Grouping(t *testing.T) {
	pl := &Plan{Lines: []ui.DiffLine{
		ui.Line(ui.Add, "volume v1 will be created"),
		ui.Line(ui.Add, "network n1 will be created"),
		ui.Line(ui.Add, "service app/s1 will be started"),
		ui.Line(ui.Add, "fileset fs: create a"),
		ui.Line(ui.Noop, "nothing to do"),
	}}
	out := ui.StripANSI(pl.String())
	if !strings.Contains(out, "Volumes") || !strings.Contains(out, "Networks") || !strings.Contains(out, "Applications") || !strings.Contains(out, "Filesets") {
		t.Fatalf("expected grouped section titles; got:\n%s", out)
	}
}
