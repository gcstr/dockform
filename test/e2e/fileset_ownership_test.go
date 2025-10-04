package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
)

func TestFilesetOwnership_NumericIDs(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}
	if out := safeOutput(exec.Command("docker", "context", "inspect", "default")); strings.TrimSpace(out) == "" {
		t.Skip("docker context 'default' not available; skipping e2e")
	}
	prevCtx := os.Getenv("DOCKER_CONTEXT")
	_ = os.Setenv("DOCKER_CONTEXT", "default")
	t.Cleanup(func() { _ = os.Setenv("DOCKER_CONTEXT", prevCtx) })

	runID := uniqueID()
	identifier := runID
	ensureNetworkCreatableOrSkip(t, identifier)

	projectDir := t.TempDir()
	bin := buildDockform(t)

	t.Cleanup(func() {
		cleanupByLabel(t, identifier)
	})

	// Create compose file with a simple service
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yaml"), []byte(fmt.Sprintf(`
services:
  app:
    image: alpine:3.22
    command: sleep 1
    labels:
      io.dockform.identifier: %s
    volumes:
      - data:/data

volumes:
  data:
    external: true
    name: "df_e2e_%s_data"
`, identifier, runID)), 0644); err != nil {
		t.Fatalf("write compose: %v", err)
	}

	// Create source files
	configDir := filepath.Join(projectDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "app.conf"), []byte("test=value\n"), 0644); err != nil {
		t.Fatalf("write app.conf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "secret.conf"), []byte("secret=data\n"), 0644); err != nil {
		t.Fatalf("write secret.conf: %v", err)
	}

	// Create manifest with ownership settings using numeric IDs
	manifest := fmt.Sprintf(`
docker:
  identifier: %s
  context: default

applications:
  app:
    root: .
    files:
      - docker-compose.yaml
    project:
      name: df_e2e_%s

filesets:
  config:
    source: ./config
    target_volume: df_e2e_%s_data
    target_path: /data
    ownership:
      user: "1000"
      group: "1000"
      file_mode: "0640"
      dir_mode: "0750"
`, identifier, runID, runID)

	manifestPath := filepath.Join(projectDir, "dockform.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	env := os.Environ()

	// Apply the manifest
	applyOut := runCmdWithStdin(t, projectDir, env, bin, "yes\n", "apply", "-c", manifestPath)
	t.Logf("Apply output:\n%s", applyOut)

	// Verify ownership and permissions inside the volume
	volumeName := fmt.Sprintf("df_e2e_%s_data", runID)
	out := runDocker(t, "run", "--rm", "-v", volumeName+":/data", "alpine:3.22", "sh", "-c",
		"stat -c '%u:%g %a %n' /data /data/app.conf /data/secret.conf 2>&1 || ls -la /data")

	t.Logf("Stat output:\n%s", out)

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines of output, got %d: %v", len(lines), lines)
	}

	// Check directory: /data should be 1000:1000 750
	if !strings.Contains(lines[0], "1000:1000") {
		t.Errorf("directory ownership incorrect: %s (expected 1000:1000)", lines[0])
	}
	if !strings.Contains(lines[0], "750") {
		t.Errorf("directory mode incorrect: %s (expected 750)", lines[0])
	}

	// Check files: should be 1000:1000 640
	for i := 1; i <= 2; i++ {
		if !strings.Contains(lines[i], "1000:1000") {
			t.Errorf("file ownership incorrect: %s (expected 1000:1000)", lines[i])
		}
		if !strings.Contains(lines[i], "640") {
			t.Errorf("file mode incorrect: %s (expected 640)", lines[i])
		}
	}
}

func TestFilesetOwnership_PreserveExisting(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}
	if out := safeOutput(exec.Command("docker", "context", "inspect", "default")); strings.TrimSpace(out) == "" {
		t.Skip("docker context 'default' not available; skipping e2e")
	}
	prevCtx := os.Getenv("DOCKER_CONTEXT")
	_ = os.Setenv("DOCKER_CONTEXT", "default")
	t.Cleanup(func() { _ = os.Setenv("DOCKER_CONTEXT", prevCtx) })

	ctx := context.Background()
	runID := uniqueID()
	identifier := runID
	ensureNetworkCreatableOrSkip(t, identifier)

	projectDir := t.TempDir()
	bin := buildDockform(t)

	t.Cleanup(func() {
		cleanupByLabel(t, identifier)
	})

	// Create compose file
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yaml"), []byte(fmt.Sprintf(`
services:
  app:
    image: alpine:3.22
    command: sleep 1
    labels:
      io.dockform.identifier: %s
    volumes:
      - data:/data

volumes:
  data:
    external: true
    name: "df_e2e_%s_data"
`, identifier, runID)), 0644); err != nil {
		t.Fatalf("write compose: %v", err)
	}

	// Create initial source file
	configDir := filepath.Join(projectDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "file1.txt"), []byte("content1\n"), 0644); err != nil {
		t.Fatalf("write file1: %v", err)
	}

	// Create manifest without preserve_existing (will apply to all)
	manifest := fmt.Sprintf(`
docker:
  identifier: %s
  context: default

applications:
  app:
    root: .
    files:
      - docker-compose.yaml
    project:
      name: df_e2e_%s

filesets:
  config:
    source: ./config
    target_volume: df_e2e_%s_data
    target_path: /data
    ownership:
      user: "2000"
      group: "2000"
      file_mode: "0600"
`, identifier, runID, runID)

	manifestPath := filepath.Join(projectDir, "dockform.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	env := os.Environ()

	// First apply
	_ = runCmdWithStdin(t, projectDir, env, bin, "yes\n", "apply", "-c", manifestPath)

	volumeName := fmt.Sprintf("df_e2e_%s_data", runID)

	// Verify initial ownership using dockercli
	docker := dockercli.New("default")
	result, err := docker.RunVolumeScript(ctx, volumeName, "/data", "stat -c '%u:%g' /data/file1.txt", nil)
	if err != nil {
		t.Fatalf("failed to check initial ownership: %v", err)
	}
	if !strings.Contains(result.Stdout, "2000:2000") {
		t.Fatalf("initial ownership incorrect: %s (expected 2000:2000)", result.Stdout)
	}

	// Add a new file and enable preserve_existing
	if err := os.WriteFile(filepath.Join(configDir, "file2.txt"), []byte("content2\n"), 0644); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	// Update manifest with preserve_existing and different ownership
	manifest = fmt.Sprintf(`
docker:
  identifier: %s
  context: default

applications:
  app:
    root: .
    files:
      - docker-compose.yaml
    project:
      name: df_e2e_%s

filesets:
  config:
    source: ./config
    target_volume: df_e2e_%s_data
    target_path: /data
    ownership:
      user: "3000"
      group: "3000"
      file_mode: "0644"
      preserve_existing: true
`, identifier, runID, runID)

	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("write updated manifest: %v", err)
	}

	// Second apply
	_ = runCmdWithStdin(t, projectDir, env, bin, "yes\n", "apply", "-c", manifestPath)

	// Verify: file1 should still be 2000:2000, file2 should be 3000:3000
	result, err = docker.RunVolumeScript(ctx, volumeName, "/data", "stat -c '%u:%g %n' /data/file1.txt /data/file2.txt", nil)
	if err != nil {
		t.Fatalf("failed to check final ownership: %v\nstderr: %s", err, result.Stderr)
	}

	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}

	// file1 should keep old ownership (preserve_existing=true doesn't re-apply to unchanged files)
	if !strings.Contains(lines[0], "2000:2000") {
		t.Errorf("file1 ownership changed unexpectedly: %s (expected 2000:2000)", lines[0])
	}

	// file2 should have new ownership (it's a new file)
	if !strings.Contains(lines[1], "3000:3000") {
		t.Errorf("file2 ownership incorrect: %s (expected 3000:3000)", lines[1])
	}
}
