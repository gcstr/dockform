package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/filesets"
)

func TestFilesetSyncAndIndex(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}

	ctx := context.Background()
	runID := uniqueID()
	identifier := runID

	// Prepare temp workdir by copying scenario
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "fileset")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}

	// Replace RUN_ID placeholders in config template if any
	// (scenario uses ${DOCKFORM_RUN_ID})

	// Build dockform once and reuse path
	bin := buildDockform(t)

	// Always cleanup labeled resources
	t.Cleanup(func() {
		cleanupByLabel(t, identifier)
	})

	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	// Apply to sync fileset into volume
	_ = runCmd(t, tempDir, env, bin, "apply", "-c", tempDir)

	// Verify volume exists (labeled)
	vols := dockerLines(t, ctx, "volume", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(vols) == 0 {
		t.Fatalf("expected labeled volume to be created")
	}
	// Our expected volume name is df_e2e_<id>_fsvol
	volumeName := "df_e2e_" + runID + "_fsvol"

	// Read index file from the volume
	idxJSON := readFromVolume(t, volumeName, "/data", filesets.IndexFileName)
	if strings.TrimSpace(idxJSON) == "" {
		t.Fatalf("index file not found or empty")
	}

	// Parse remote index
	remoteIdx, err := filesets.ParseIndexJSON(idxJSON)
	if err != nil {
		t.Fatalf("parse remote index: %v\n%s", err, idxJSON)
	}

	// Build local index from source and excludes
	srcDir := filepath.Join(tempDir, "files-src")
	localIdx, err := filesets.BuildLocalIndex(srcDir, "/data", []string{"ignore/", "**/*.tmp"})
	if err != nil {
		t.Fatalf("build local index: %v", err)
	}

	// Compare hashes and file lists
	if remoteIdx.TreeHash != localIdx.TreeHash {
		// For easier debugging, print diff of file lists
		lset := make([]string, 0, len(localIdx.Files))
		rset := make([]string, 0, len(remoteIdx.Files))
		for _, f := range localIdx.Files {
			lset = append(lset, f.Path)
		}
		for _, f := range remoteIdx.Files {
			rset = append(rset, f.Path)
		}
		sort.Strings(lset)
		sort.Strings(rset)
		b, _ := json.Marshal(struct{ Local, Remote []string }{Local: lset, Remote: rset})
		t.Fatalf("tree hash mismatch; lists: %s", string(b))
	}

	// Ensure included files are present in the target volume
	for _, f := range localIdx.Files {
		p := f.Path
		chk := runDocker(t, "run", "--rm", "-v", volumeName+":/data", "alpine", "sh", "-c", "test -f '"+filepath.Join("/data", p)+"' && echo OK || echo MISSING")
		if !strings.Contains(chk, "OK") {
			t.Fatalf("expected file present in volume: %s\n%s", p, chk)
		}
	}

	// Ensure excluded paths are not present in the volume
	// - directory ignore/
	// - any *.tmp
	// Try to stat excluded file inside the container; expect missing
	out := runDocker(t, "run", "--rm", "-v", volumeName+":/data", "alpine", "sh", "-c", "test ! -e /data/ignore/secret.txt && echo OK || echo FOUND")
	if !strings.Contains(out, "OK") {
		t.Fatalf("excluded file present in volume: %s", out)
	}
	out2 := runDocker(t, "run", "--rm", "-v", volumeName+":/data", "alpine", "sh", "-c", "! ls -1 /data | grep -q '\\.tmp$' && echo OK || echo FOUND")
	if !strings.Contains(out2, "OK") {
		t.Fatalf("tmp files present in volume: %s", out2)
	}
}

func readFromVolume(t *testing.T, volumeName, targetPath, relFile string) string {
	t.Helper()
	cmd := []string{"run", "--rm", "-v", volumeName + ":" + targetPath, "alpine", "sh", "-c", "cat '" + filepath.Join(targetPath, relFile) + "'"}
	return runDocker(t, cmd...)
}

func runDocker(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %v failed: %v\n%s", args, err, string(out))
	}
	return string(out)
}
