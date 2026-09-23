package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
)

// A volume declared with destroy: false survives a real destroy with its data
// intact, while every other managed resource is removed.
func TestDestroy_KeepsVolumeMarkedDestroyFalse(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}
	ctx := context.Background()
	_ = logDockerContext(t)
	if looksProd(t) && os.Getenv("E2E_ALLOW_HOST") != "1" {
		t.Skip("refusing to run e2e against a production-looking daemon; set E2E_ALLOW_HOST=1 to override")
	}

	runID := uniqueID()
	identifier := runID
	ensureNetworkCreatableOrSkip(t, identifier)
	bin := buildDockform(t)
	t.Cleanup(func() { cleanupByLabel(t, identifier) }) // also removes the kept volume

	keptVol := "df_e2e_" + runID + "_vol"
	_ = exec.Command("docker", "volume", "create", "--label", "io.dockform.identifier="+identifier, keptVol).Run()
	runDocker(t, "run", "--rm", "-v", keptVol+":/data", dockercli.HelperImage, "sh", "-c", "echo keep-me > /data/marker")

	tempDir := t.TempDir()
	if err := copyTree(filepath.Join("testdata", "scenarios", "simple"), tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}
	manifest := `identifier: ${DOCKFORM_RUN_ID}

contexts:
  default:
    volumes:
      df_e2e_${DOCKFORM_RUN_ID}_vol:
        destroy: false
    networks:
      df_e2e_${DOCKFORM_RUN_ID}_net: {}

stacks:
  default/app:
    root: .
    files:
      - docker-compose.yaml
    project:
      name: df_e2e_${DOCKFORM_RUN_ID}
`
	if err := os.WriteFile(filepath.Join(tempDir, "dockform.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	if stdout, stderr, code := runCmdWithStdinDetailed(t, tempDir, env, bin, "yes\n", "apply", "--manifest", tempDir); code != 0 {
		t.Fatalf("apply failed (%d)\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}

	out, errOut, code := runCmdDetailed(t, tempDir, env, bin, "destroy", "--skip-confirmation", "--manifest", tempDir)
	if code != 0 {
		t.Fatalf("destroy failed (%d)\nSTDOUT:\n%s\nSTDERR:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "kept (destroy: false)") {
		t.Errorf("destroy output should list the kept volume:\n%s", out)
	}

	label := "label=io.dockform.identifier=" + identifier
	if c := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", label); len(c) != 0 {
		t.Errorf("containers survived destroy: %v", c)
	}
	if n := dockerLines(t, ctx, "network", "ls", "--format", "{{.Name}}", "--filter", label); len(n) != 0 {
		t.Errorf("networks survived destroy: %v", n)
	}
	vols := dockerLines(t, ctx, "volume", "ls", "--format", "{{.Name}}", "--filter", label)
	if len(vols) != 1 || vols[0] != keptVol {
		t.Fatalf("after destroy want only the kept volume %s, got %v", keptVol, vols)
	}
	if got := strings.TrimSpace(readFromVolume(t, keptVol, "/data", "marker")); got != "keep-me" {
		t.Errorf("kept volume lost its data: marker = %q, want keep-me", got)
	}
}
