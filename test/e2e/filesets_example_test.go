package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gcstr/dockform/internal/filesets"
)

// This suite validates fileset flows against the example manifest in ./example

func TestScenarioFilesets_InitialSync_Index(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}
	// Ensure the 'default' docker context exists since empty context is normalized to 'default'
	if out := safeOutput(exec.Command("docker", "context", "inspect", "default")); strings.TrimSpace(out) == "" {
		t.Skip("docker context 'default' not available; skipping e2e")
	}
	prevCtx := os.Getenv("DOCKER_CONTEXT")
	_ = os.Setenv("DOCKER_CONTEXT", "default")
	t.Cleanup(func() { _ = os.Setenv("DOCKER_CONTEXT", prevCtx) })

	_ = context.Background()
	runID := uniqueID()
	identifier := runID
	ensureNetworkCreatableOrSkip(t, identifier)

	// Clean any leftover demo resources
	t.Cleanup(func() {
		cleanupByLabel(t, identifier)
	})

	bin := buildDockform(t)
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "fileset")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}
	cfgPath := tempDir

	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	// Apply full config to create resources and sync fileset
	out, errOut, code := runCmdDetailed(t, tempDir, env, bin, "apply", "--skip-confirmation", "-c", cfgPath)
	if code != 0 {
		t.Fatalf("apply failed: %d\nSTDOUT:\n%s\nSTDERR:\n%s", code, out, errOut)
	}

	// Assert files were copied and index is present
	volName := "df_e2e_" + runID + "_fsvol"
	chk := runDocker(t, "run", "--rm", "-v", volName+":/data", "alpine", "sh", "-c", "test -f /data/file1.txt && echo OK || echo MISSING")
	if !strings.Contains(chk, "OK") {
		t.Fatalf("expected asset present in volume: %s", chk)
	}

	// Read and parse the index
	idxJSON := readFromVolume(t, volName, "/data", filesets.IndexFileName)
	if strings.TrimSpace(idxJSON) == "" {
		t.Fatalf("index file missing or empty")
	}
	idx, err := filesets.ParseIndexJSON(idxJSON)
	if err != nil {
		t.Fatalf("parse index: %v\n%s", err, idxJSON)
	}
	if strings.TrimSpace(idx.TreeHash) == "" {
		t.Fatalf("tree_hash should not be empty")
	}
	// Ensure index JSON is valid JSON (redundant but helpful)
	var tmp map[string]any
	if err := json.Unmarshal([]byte(idxJSON), &tmp); err != nil {
		t.Fatalf("index is not valid json: %v", err)
	}
}

func TestScenarioFilesets_ChangeDetection_And_Restart(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}

	// Ensure the 'default' docker context exists since empty context is normalized to 'default'
	if out := safeOutput(exec.Command("docker", "context", "inspect", "default")); strings.TrimSpace(out) == "" {
		t.Skip("docker context 'default' not available; skipping e2e")
	}
	prevCtx := os.Getenv("DOCKER_CONTEXT")
	_ = os.Setenv("DOCKER_CONTEXT", "default")
	t.Cleanup(func() { _ = os.Setenv("DOCKER_CONTEXT", prevCtx) })

	ctx := context.Background()
	runID := uniqueID()
	identifier := runID
	t.Cleanup(func() { cleanupByLabel(t, identifier) })
	ensureNetworkCreatableOrSkip(t, identifier)

	bin := buildDockform(t)
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "fileset_restart")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}
	cfgPath := tempDir
	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	// First apply to ensure baseline
	_, _, code := runCmdDetailed(t, tempDir, env, bin, "apply", "--skip-confirmation", "-c", cfgPath)
	if code != 0 {
		t.Fatalf("initial apply failed")
	}

	// Capture sleeper container name and start time
	names := dockerLines(t, ctx, "ps", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	var sleeperName string
	for _, n := range names {
		if strings.Contains(n, "sleeper") {
			sleeperName = n
			break
		}
	}
	if sleeperName == "" {
		t.Fatalf("sleeper container not found")
	}
	started0 := safeOutput(exec.Command("docker", "inspect", "-f", "{{.State.StartedAt}}", sleeperName))
	if strings.TrimSpace(started0) == "" {
		t.Fatalf("could not read initial StartedAt")
	}

	// Modify a source file under scenario assets
	img := filepath.Join(tempDir, "files-src", "img", "df.svg")
	// Append a harmless comment to change hash
	f, err := os.OpenFile(img, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open asset: %v", err)
	}
	_, _ = f.WriteString("\n<!-- e2e change -->\n")
	_ = f.Close()

	// Give filesystem a moment to tick mtime to avoid same-second hashes on some FS
	time.Sleep(200 * time.Millisecond)

	// Run filesets apply only
	out, errOut, code2 := runCmdDetailed(t, tempDir, env, bin, "filesets", "apply", "-c", cfgPath)
	if code2 != 0 {
		t.Fatalf("filesets apply failed: %d\nSTDOUT:\n%s\nSTDERR:\n%s", code2, out, errOut)
	}
	// Expect output to include update for the modified path (filtered to filesets only)
	if !strings.Contains(out, "fileset files: update img/df.svg") {
		// Fallback: just check for 'update df.svg'
		if !strings.Contains(out, "update df.svg") {
			t.Fatalf("expected fileset update in output, got:\n%s", out)
		}
	}

	// Container should be restarted due to restart_services: [nginx]
	// Wait briefly to allow restart
	time.Sleep(1 * time.Second)
	started1 := safeOutput(exec.Command("docker", "inspect", "-f", "{{.State.StartedAt}}", sleeperName))
	if strings.TrimSpace(started1) == "" {
		t.Fatalf("could not read restarted StartedAt")
	}
	if started1 == started0 {
		t.Fatalf("expected sleeper container to be restarted; start time did not change. before=%s after=%s", started0, started1)
	}
}

func TestScenarioFilesets_Excludes_Behavior(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}

	// Ensure the 'default' docker context exists since empty context is normalized to 'default'
	if out := safeOutput(exec.Command("docker", "context", "inspect", "default")); strings.TrimSpace(out) == "" {
		t.Skip("docker context 'default' not available; skipping e2e")
	}
	prevCtx := os.Getenv("DOCKER_CONTEXT")
	_ = os.Setenv("DOCKER_CONTEXT", "default")
	t.Cleanup(func() { _ = os.Setenv("DOCKER_CONTEXT", prevCtx) })

	runID := uniqueID()
	identifier := runID
	t.Cleanup(func() { cleanupByLabel(t, identifier) })
	ensureNetworkCreatableOrSkip(t, identifier)

	bin := buildDockform(t)
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "fileset_restart")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}
	cfgPath := tempDir
	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	// Ensure base apply
	_, _, code := runCmdDetailed(t, tempDir, env, bin, "apply", "--skip-confirmation", "-c", cfgPath)
	if code != 0 {
		t.Fatalf("initial apply failed")
	}

	// Create files that should be excluded per example excludes: tmp/**, secrets/, *.bak
	assetsRoot := filepath.Join(tempDir, "files-src")
	mustMkdirAll(t, filepath.Join(assetsRoot, "tmp"))
	mustMkdirAll(t, filepath.Join(assetsRoot, "secrets"))
	mustWriteFile(t, filepath.Join(assetsRoot, "tmp", "foo.txt"), "tmp file")
	mustWriteFile(t, filepath.Join(assetsRoot, "secrets", "x.txt"), "secret")
	mustWriteFile(t, filepath.Join(assetsRoot, "a.bak"), "backup")

	// Re-apply filesets only
	_, _, code2 := runCmdDetailed(t, tempDir, env, bin, "filesets", "apply", "-c", cfgPath)
	if code2 != 0 {
		t.Fatalf("filesets apply failed")
	}

	// Assert excluded paths are not present in volume
	volName := "df_e2e_" + runID + "_fsvol2"
	out1 := runDocker(t, "run", "--rm", "-v", volName+":/assets", "alpine", "sh", "-c", "test ! -e /assets/tmp/foo.txt && echo OK || echo FOUND")
	if !strings.Contains(out1, "OK") {
		t.Fatalf("excluded tmp/foo.txt present: %s", out1)
	}
	out2 := runDocker(t, "run", "--rm", "-v", volName+":/assets", "alpine", "sh", "-c", "test ! -e /assets/secrets/x.txt && echo OK || echo FOUND")
	if !strings.Contains(out2, "OK") {
		t.Fatalf("excluded secrets/x.txt present: %s", out2)
	}
	out3 := runDocker(t, "run", "--rm", "-v", volName+":/assets", "alpine", "sh", "-c", "test ! -e /assets/a.bak && echo OK || echo FOUND")
	if !strings.Contains(out3, "OK") {
		t.Fatalf("excluded a.bak present: %s", out3)
	}

	// Index excludes should include normalized patterns
	idxJSON := readFromVolume(t, volName, "/assets", filesets.IndexFileName)
	idx, err := filesets.ParseIndexJSON(idxJSON)
	if err != nil {
		t.Fatalf("parse index: %v", err)
	}
	// normalizeExcludePatterns turns patterns into slash form and appends /** for directories
	// From example: "tmp/**", "secrets/" -> becomes "secrets/**"
	has := func(slice []string, v string) bool {
		for _, s := range slice {
			if s == v {
				return true
			}
		}
		return false
	}
	if !has(idx.Exclude, "tmp/**") {
		t.Fatalf("expected normalized exclude tmp/** in index; got: %v", idx.Exclude)
	}
	if !has(idx.Exclude, "secrets/**") {
		t.Fatalf("expected normalized exclude secrets/** in index; got: %v", idx.Exclude)
	}
	if !has(idx.Exclude, "*.bak") {
		t.Fatalf("expected normalized exclude *.bak in index; got: %v", idx.Exclude)
	}
}

func mustMkdirAll(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
}

func mustWriteFile(t *testing.T, p string, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
