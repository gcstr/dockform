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

func writeDockerStubFile(t *testing.T, dir string, script string) string {
	t.Helper()
	path := filepath.Join(dir, "docker")
	if runtime.GOOS == "windows" {
		path += ".cmd"
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

func withPlannerDockerStub_Basic(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "vOld"; exit 0; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "nOld"; exit 0; fi ;;
  compose)
    # services
    for a in "$@"; do [ "$a" = "--services" ] && { echo "nginx"; exit 0; }; done
    # json config
    saw_config=0; saw_format=0; jsonfmt=0
    for a in "$@"; do
      [ "$a" = "config" ] && saw_config=1
      [ "$a" = "--format" ] && saw_format=1
      [ "$a" = "json" ] && jsonfmt=1
    done
    if [ $saw_config -eq 1 ] && [ $saw_format -eq 1 ] && [ $jsonfmt -eq 1 ]; then
      echo '{"services":{"nginx":{}}}'
      exit 0
    fi
    # hash (find service after --hash)
    for i in "$@"; do :; done
    idx=0; svc=""
    for a in "$@"; do
      if [ "$a" = "--hash" ]; then
        shift
        svc="$1"; [ -n "$svc" ] || svc="nginx"; echo "$svc deadbeef"; exit 0
      fi
    done
    # ps json (find tokens anywhere)
    saw_ps=0; saw_format=0; jsonfmt=0
    for a in "$@"; do
      [ "$a" = "ps" ] && saw_ps=1
      [ "$a" = "--format" ] && saw_format=1
      [ "$a" = "json" ] && jsonfmt=1
    done
    if [ $saw_ps -eq 1 ] && [ $saw_format -eq 1 ] && [ $jsonfmt -eq 1 ]; then echo "[]"; exit 0; fi
    # up -d (anywhere)
    saw_up=0; saw_d=0
    for a in "$@"; do [ "$a" = "up" ] && saw_up=1; [ "$a" = "-d" ] && saw_d=1; done
    if [ $saw_up -eq 1 ] && [ $saw_d -eq 1 ]; then exit 0; fi
    exit 0 ;;
  ps)
    # docker ps -a --format used by ListComposeContainersAll
    echo "proj;other;other_name"
    exit 0 ;;
  inspect)
    echo "{}"; exit 0 ;;
esac
exit 0
`
	_ = writeDockerStubFile(t, dir, script)
	old := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+old); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return func() { _ = os.Setenv("PATH", old) }
}

func withPlannerDockerStub_Mismatch(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo ""; exit 0; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo ""; exit 0; fi ;;
  compose)
    for a in "$@"; do [ "$a" = "--services" ] && { echo "nginx"; exit 0; }; done
    saw_config=0; saw_format=0; jsonfmt=0
    for a in "$@"; do [ "$a" = "config" ] && saw_config=1; [ "$a" = "--format" ] && saw_format=1; [ "$a" = "json" ] && jsonfmt=1; done
    if [ $saw_config -eq 1 ] && [ $saw_format -eq 1 ] && [ $jsonfmt -eq 1 ]; then echo '{"services":{"nginx":{}}}'; exit 0; fi
    saw_ps=0; saw_format=0; jsonfmt=0
    for a in "$@"; do [ "$a" = "ps" ] && saw_ps=1; [ "$a" = "--format" ] && saw_format=1; [ "$a" = "json" ] && jsonfmt=1; done
    if [ $saw_ps -eq 1 ] && [ $saw_format -eq 1 ] && [ $jsonfmt -eq 1 ]; then echo '[{"Name":"c1","Service":"nginx"}]'; exit 0; fi
    for a in "$@"; do if [ "$a" = "--hash" ]; then echo "nginx deadbeef"; exit 0; fi; done
    exit 0 ;;
  inspect)
    # Labels: different identifier and a hash
    echo '{"io.dockform/oldid":"1","com.docker.compose.config-hash":"cafebabe"}'
    exit 0 ;;
esac
exit 0
`
	_ = writeDockerStubFile(t, dir, script)
	old := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+old); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return func() { _ = os.Setenv("PATH", old) }
}

func withPlannerDockerStub_VolumeLsError(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "boom" 1>&2; exit 1; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo ""; exit 0; fi ;;
  compose)
    if [ "$1" = "ps" ] && [ "$2" = "--format" ] && [ "$3" = "json" ]; then echo "[]"; exit 0; fi
    exit 0 ;;
esac
exit 0
`
	_ = writeDockerStubFile(t, dir, script)
	old := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+old); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return func() { _ = os.Setenv("PATH", old) }
}

func TestBuildPlan_WithDocker_AddsAndRemoves(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows due to shell script compatibility")
	}
	defer withPlannerDockerStub_Basic(t)()
	cfg := manifest.Config{
		Docker: manifest.DockerConfig{Context: "", Identifier: "demo"},
		Stacks: map[string]manifest.Stack{
			"app": {Root: t.TempDir(), Files: []string{"compose.yml"}},
		},
		Filesets: map[string]manifest.FilesetSpec{"data": {Source: "src", TargetVolume: "v1", TargetPath: "/app"}},
		Networks: map[string]manifest.NetworkSpec{"n1": {}},
	}
	d := dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
	pln, err := NewWithDocker(d).BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	out := pln.String()
	// Check volume/network adds and removals (new icon-based UI)
	mustContain(t, out, "↑ v1 will be created")
	mustContain(t, out, "× vOld will be deleted")
	mustContain(t, out, "↑ n1 will be created")
	mustContain(t, out, "× nOld will be deleted")
	// Service to be created (now in nested format)
	mustContain(t, out, "↑ nginx will be created")
	// Unmanaged running service should appear under Applications for deletion
	mustContain(t, out, "× other will be deleted")
}

func TestBuildPlan_IdentifierMismatch_Reconciles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows due to shell script compatibility")
	}
	defer withPlannerDockerStub_Mismatch(t)()
	cfg := manifest.Config{
		Docker: manifest.DockerConfig{Context: "", Identifier: "demo"},
		Stacks: map[string]manifest.Stack{
			"app": {Root: t.TempDir(), Files: []string{"compose.yml"}},
		},
	}
	d := dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
	pln, err := NewWithDocker(d).BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	out := pln.String()
	mustContain(t, out, "→ nginx will be reconciled (identifier mismatch)")
}

func TestBuildPlan_ExplicitVolumes_HandledCorrectly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows due to shell script compatibility")
	}
	defer withPlannerDockerStub_Basic(t)()
	cfg := manifest.Config{
		Docker: manifest.DockerConfig{Context: "", Identifier: "demo"},
		Stacks: map[string]manifest.Stack{
			"app": {Root: t.TempDir(), Files: []string{"compose.yml"}},
		},
		// Mix of explicit volumes and volumes from filesets
		Volumes:  map[string]manifest.TopLevelResourceSpec{"explicit-vol": {}, "shared-data": {}},
		Filesets: map[string]manifest.FilesetSpec{"data": {Source: "src", TargetVolume: "fileset-vol", TargetPath: "/app"}},
		Networks: map[string]manifest.NetworkSpec{"n1": {}},
	}
	d := dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
	pln, err := NewWithDocker(d).BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	out := pln.String()
	// Check that all volumes are properly handled:
	// 1. Explicit volumes should be created
	mustContain(t, out, "↑ explicit-vol will be created")
	mustContain(t, out, "↑ shared-data will be created")
	// 2. Volumes from filesets should also be created
	mustContain(t, out, "↑ fileset-vol will be created")
	// 3. Old volume should be removed
	mustContain(t, out, "× vOld will be deleted")
	// Networks should work as before
	mustContain(t, out, "↑ n1 will be created")
	mustContain(t, out, "× nOld will be deleted")
	// Service should work as before (now in nested format)
	mustContain(t, out, "↑ nginx will be created")
}

func TestApply_PropagatesVolumeListError(t *testing.T) {
	defer withPlannerDockerStub_VolumeLsError(t)()
	cfg := manifest.Config{
		Docker: manifest.DockerConfig{Context: "", Identifier: "demo"},
		Stacks: map[string]manifest.Stack{},
	}
	d := dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
	err := NewWithDocker(d).Apply(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "list volumes") {
		t.Fatalf("expected list volumes error, got: %v", err)
	}
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	norm := func(x string) string { return strings.Join(strings.Fields(x), " ") }
	if !strings.Contains(norm(s), norm(sub)) {
		t.Fatalf("expected to contain %q; got:\n%s", sub, s)
	}
}
