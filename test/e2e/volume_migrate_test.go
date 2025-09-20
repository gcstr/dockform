package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVolumeMigration_RecreateWithData ensures that when a manifest specifies migrate: true and
// desired driver/options differ from the existing volume, Dockform migrates data and replaces the volume.
func TestVolumeMigration_RecreateWithData(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}
	_ = logDockerContext(t)

	ctx := context.Background()
	runID := uniqueID()
	identifier := runID

	// Create an external volume with different options (bind mount to a temp dir)
	hostDir := filepath.Join(os.TempDir(), "df_volmigrate_"+runID)
	_ = os.MkdirAll(hostDir, 0o755)
	volName := "df_e2e_" + runID + "_vol"
	_ = exec.Command("docker", "volume", "create",
		"--label", "io.dockform.identifier="+identifier,
		"--opt", "type=none",
		"--opt", "o=bind",
		"--opt", "device="+hostDir,
		volName,
	).Run()

	// Write a marker file into the old volume
	_ = exec.Command("docker", "run", "--rm", "-v", volName+":/data", "alpine", "sh", "-c", "echo hello > /data/marker.txt").Run()

	// Prepare simple scenario workspace
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "simple")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}

	bin := buildDockform(t)
	env := append([]string{}, os.Environ()...)
	env = append(env, "DOCKFORM_RUN_ID="+runID)

	// Modify manifest to include volumes entry with migrate: true and desired options (none)
	manifestPath := filepath.Join(tempDir, "dockform.yml")
	b, err := os.ReadFile(manifestPath)
	if err == nil {
		s := string(b)
		// Insert volumes section under networks or after it
		if strings.Contains(s, "\nnetworks:") && strings.Contains(s, "df_e2e_${DOCKFORM_RUN_ID}_net:") {
			s = strings.Replace(s,
				"networks:\n  df_e2e_${DOCKFORM_RUN_ID}_net: {}",
				"networks:\n  df_e2e_${DOCKFORM_RUN_ID}_net: {}\n\nvolumes:\n  df_e2e_${DOCKFORM_RUN_ID}_vol:\n    driver: local\n    migrate: true",
				1,
			)
		} else {
			s += "\nvolumes:\n  df_e2e_${DOCKFORM_RUN_ID}_vol:\n    driver: local\n    migrate: true\n"
		}
		_ = os.WriteFile(manifestPath, []byte(s), 0o644)
	}

	// Plan & Apply (skip confirmation)
	_ = runCmd(t, tempDir, env, bin, "plan", "-c", tempDir)
	_ = runCmd(t, tempDir, env, bin, "apply", "--skip-confirmation", "-c", tempDir)

	// Verify the container came up
	names := dockerLines(t, ctx, "ps", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(names) == 0 {
		t.Fatalf("expected running container with label io.dockform.identifier=%s", identifier)
	}

	// Verify the marker file still exists in the volume after migration
	out := safeOutput(exec.Command("docker", "run", "--rm", "-v", volName+":/data", "alpine", "sh", "-c", "cat /data/marker.txt || true"))
	if strings.TrimSpace(out) != "hello" {
		t.Fatalf("expected marker to be migrated, got: %q", out)
	}

	// Cleanup
	_ = runCmd(t, tempDir, env, bin, "destroy", "--skip-confirmation", "-c", tempDir)
}
