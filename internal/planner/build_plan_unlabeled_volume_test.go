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

// withDockerStub_UnlabeledVolume stubs a daemon where the volume "legacy"
// exists but carries no io.dockform.identifier label: a filtered `volume ls`
// (what discovery uses) does not see it, an unfiltered one does.
func withDockerStub_UnlabeledVolume(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then
      for a in "$@"; do
        case "$a" in
          label=*) exit 0 ;;
        esac
      done
      echo "legacy"
      exit 0
    fi
    if [ "$sub" = "create" ]; then echo "created"; exit 0; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then exit 0; fi ;;
  ps) echo ""; exit 0 ;;
  context) echo ""; exit 0 ;;
esac
exit 0
`
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	old := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	return func() { _ = os.Setenv("PATH", old) }
}

func unlabeledVolumeConfig() manifest.Config {
	return manifest.Config{
		Identifier: "demo",
		Contexts: map[string]manifest.ContextConfig{
			"default": {Volumes: map[string]manifest.TopLevelResourceSpec{"legacy": {}}},
		},
		Stacks: map[string]manifest.Stack{},
	}
}

func TestBuildPlan_UnlabeledExistingVolume_NotPlannedForCreation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows due to shell script compatibility")
	}
	defer withDockerStub_UnlabeledVolume(t)()

	d := dockercli.New("").WithIdentifier("demo")
	pln, err := NewWithDocker(d).BuildPlan(context.Background(), unlabeledVolumeConfig())
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	out := pln.String()
	if strings.Contains(out, "legacy will be created") {
		t.Errorf("volume that already exists unlabeled must not be planned for creation; plan was:\n%s", out)
	}
	if !strings.Contains(out, "unlabeled") {
		t.Errorf("plan should report the volume as existing but unlabeled; plan was:\n%s", out)
	}
}

// withDockerStub_UnlabeledVolumeRecording behaves like the stub above but
// appends every invocation to a log file so the test can assert which docker
// commands apply actually issued.
func withDockerStub_UnlabeledVolumeRecording(t *testing.T) (func(), string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
echo "$@" >> "` + logPath + `"
cmd="$1"; shift
case "$cmd" in
  volume)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then
      for a in "$@"; do
        case "$a" in
          label=*) exit 0 ;;
        esac
      done
      echo "legacy"
      exit 0
    fi
    if [ "$sub" = "create" ]; then echo "created"; exit 0; fi ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "ls" ]; then exit 0; fi ;;
  ps) echo ""; exit 0 ;;
  context) echo ""; exit 0 ;;
esac
exit 0
`
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	old := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	return func() { _ = os.Setenv("PATH", old) }, logPath
}

func TestApply_UnlabeledExistingVolume_NotCreated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows due to shell script compatibility")
	}
	restore, logPath := withDockerStub_UnlabeledVolumeRecording(t)
	defer restore()

	d := dockercli.New("").WithIdentifier("demo")
	if err := NewWithDocker(d).Apply(context.Background(), unlabeledVolumeConfig()); err != nil {
		t.Fatalf("apply: %v", err)
	}

	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	for _, line := range strings.Split(string(calls), "\n") {
		if strings.HasPrefix(line, "volume create") {
			t.Errorf("apply must not run a create for a volume that already exists: %q", line)
		}
	}
}

// A volume that exists unlabeled is still a usable volume, so it must remain in
// the set EnsureVolumesExistForContext returns — that set gates fileset sync.
func TestEnsureVolumes_UnlabeledVolumeStaysAvailableForFilesets(t *testing.T) {
	m := newMockDocker()
	m.volumes = []string{}            // nothing carries the identifier label
	m.allVolumes = []string{"legacy"} // but the volume does exist

	rm := NewResourceManagerWithClient(m, nil)
	existing, err := rm.EnsureVolumesExistForContext(context.Background(), unlabeledVolumeConfig(), "default", map[string]string{})
	if err != nil {
		t.Fatalf("ensure volumes: %v", err)
	}

	if _, ok := existing["legacy"]; !ok {
		t.Error("unlabeled volume must be reported as existing so filesets targeting it still sync")
	}
	if len(m.createdVolumes) != 0 {
		t.Errorf("no volume should have been created, got %v", m.createdVolumes)
	}
}

// The unfiltered volume list must never feed orphan detection: an unlabeled
// volume dockform does not manage is not dockform's to delete.
func TestBuildPlan_UnlabeledVolume_NotPlannedForDeletion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows due to shell script compatibility")
	}
	defer withDockerStub_UnlabeledVolume(t)()

	cfg := unlabeledVolumeConfig()
	cfg.Contexts["default"] = manifest.ContextConfig{} // nothing declared at all

	d := dockercli.New("").WithIdentifier("demo")
	pln, err := NewWithDocker(d).BuildPlan(context.Background(), cfg)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	if out := pln.String(); strings.Contains(out, "legacy will be deleted") {
		t.Errorf("an unlabeled volume must never be planned for deletion; plan was:\n%s", out)
	}
}
