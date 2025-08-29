package planner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/assets"
	"github.com/gcstr/dockform/internal/config"
	"github.com/gcstr/dockform/internal/dockercli"
)

// withAssetsDockerStub installs a minimal docker stub that supports the subset
// of commands used by assets planning/apply. It uses REMOTE_JSON env to serve
// the remote manifest content and DOCKER_STUB_LOG to log operations.
func withAssetsDockerStub(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
log="$DOCKER_STUB_LOG"
cmd="$1"; shift
case "$cmd" in
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then echo "data"; exit 0; fi ;;
  run)
    # WriteFileToVolume: detect cat > and log
    for a in "$@"; do echo "$a" | grep -q "cat > "; if [ $? -eq 0 ]; then echo "write_manifest" >> "$log"; exit 0; fi; done
    # ReadFileFromVolume: cat manifest (non-redirect)
    for a in "$@"; do echo "$a" | grep -q "cat "; if [ $? -eq 0 ]; then printf '%s' "$REMOTE_JSON"; exit 0; fi; done
    # Extract tar
    for a in "$@"; do echo "$a" | grep -q "tar -xpf" && { echo "extract" >> "$log"; exit 0; }; done
    # Remove paths
    for a in "$@"; do echo "$a" | grep -q "xargs -0 rm -rf" && { echo "rm_paths" >> "$log"; exit 0; }; done
    exit 0 ;;
  ps)
    # ListComposeContainersAll
    echo "proj;nginx;app_nginx_1"
    exit 0 ;;
  container)
    sub="$1"; shift
    if [ "$sub" = "restart" ]; then echo "restart $1" >> "$log"; exit 0; fi ;;
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
	oldPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	return func() { _ = os.Setenv("PATH", oldPath) }
}

func TestBuildPlan_Assets_DiffChanges(t *testing.T) {
	// Prepare local assets: a.txt (content A), b.txt (content B)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	local, err := assets.BuildLocalManifest(src, "/target", nil)
	if err != nil {
		t.Fatalf("build local manifest: %v", err)
	}

	// Remote manifest: c.txt extra (should delete). Keep a.txt absent so it appears as create
	remote := assets.Manifest{Version: "v1", Target: "/target", Files: []assets.FileEntry{
		{Path: "c.txt", Size: 1, Sha256: "cafebabe"},
	}}
	remoteJSON, err := remote.ToJSON()
	if err != nil {
		t.Fatalf("marshal remote: %v", err)
	}

	log := filepath.Join(t.TempDir(), "log.txt")
	undo := withAssetsDockerStub(t)
	defer undo()
	_ = os.Setenv("DOCKER_STUB_LOG", log)
	_ = os.Setenv("REMOTE_JSON", remoteJSON)
	defer func() { _ = os.Unsetenv("DOCKER_STUB_LOG"); _ = os.Unsetenv("REMOTE_JSON") }()

	cfg := config.Config{Docker: config.DockerConfig{Identifier: ""}, Assets: map[string]config.AssetSpec{
		"web": {SourceAbs: src, TargetVolume: "data", TargetPath: "/target"},
	}}
	d := dockercli.New("")
	pln, err := NewWithDocker(d).BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	out := pln.String()
	// Expect create for both local files and delete for c.txt
	mustContain(t, out, "asset web: create a.txt")
	mustContain(t, out, "asset web: create b.txt")
	mustContain(t, out, "asset web: delete c.txt")
	_ = local // silence unused in case of future ref
}

func TestBuildPlan_Assets_NoChanges(t *testing.T) {
	// Local and remote are equal
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	local, err := assets.BuildLocalManifest(src, "/site", nil)
	if err != nil {
		t.Fatalf("build local: %v", err)
	}
	remoteJSON, err := local.ToJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	log := filepath.Join(t.TempDir(), "log.txt")
	undo := withAssetsDockerStub(t)
	defer undo()
	_ = os.Setenv("DOCKER_STUB_LOG", log)
	_ = os.Setenv("REMOTE_JSON", remoteJSON)
	defer func() { _ = os.Unsetenv("DOCKER_STUB_LOG"); _ = os.Unsetenv("REMOTE_JSON") }()

	cfg := config.Config{Volumes: map[string]config.TopLevelResourceSpec{"data": {}}, Assets: map[string]config.AssetSpec{
		"site": {SourceAbs: src, TargetVolume: "data", TargetPath: "/site"},
	}}
	d := dockercli.New("")
	pln, err := NewWithDocker(d).BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	out := pln.String()
	if !strings.Contains(out, "asset site: no file changes") {
		t.Fatalf("expected no file changes line; got:\n%s", out)
	}
}

func TestApply_Assets_SyncAndRestart(t *testing.T) {
	// Local has foo.txt; remote has bar.txt -> expect create foo, delete bar, write manifest, then restart
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "foo.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	local, err := assets.BuildLocalManifest(src, "/opt/data", nil)
	if err != nil {
		t.Fatalf("build local: %v", err)
	}
	// Remote contains only bar.txt
	remote := assets.Manifest{Version: "v1", Target: "/opt/data", Files: []assets.FileEntry{{Path: "bar.txt", Size: 1, Sha256: "abcd"}}}
	remoteJSON, err := remote.ToJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	log := filepath.Join(t.TempDir(), "log.txt")
	undo := withAssetsDockerStub(t)
	defer undo()
	_ = os.Setenv("DOCKER_STUB_LOG", log)
	_ = os.Setenv("REMOTE_JSON", remoteJSON)
	defer func() { _ = os.Unsetenv("DOCKER_STUB_LOG"); _ = os.Unsetenv("REMOTE_JSON") }()

	cfg := config.Config{
		Docker:   config.DockerConfig{Identifier: "demo"},
		Assets:   map[string]config.AssetSpec{"data": {SourceAbs: src, TargetVolume: "data", TargetPath: "/opt/data", RestartServices: []string{"nginx"}}},
		Volumes:  map[string]config.TopLevelResourceSpec{},
		Networks: map[string]config.TopLevelResourceSpec{},
	}
	d := dockercli.New("").WithIdentifier("demo")
	if err := NewWithDocker(d).Apply(context.Background(), cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
	b, _ := os.ReadFile(log)
	s := string(b)
	// At least these operations should have been logged
	if !strings.Contains(s, "extract") {
		t.Fatalf("expected tar extract logged; got: %s", s)
	}
	if !strings.Contains(s, "write_manifest") {
		t.Fatalf("expected manifest write logged; got: %s", s)
	}
	if !strings.Contains(s, "restart app_nginx_1") && !strings.Contains(s, "restart nginx") {
		t.Fatalf("expected restart logged; got: %s", s)
	}
	_ = local // silence unused in case of future ref
}
