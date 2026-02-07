package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	age "filippo.io/age"
)

func TestSopsSecretsEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}
	if _, err := exec.LookPath("sops"); err != nil {
		t.Skip("sops not found in PATH; skipping SOPS e2e test")
	}

	// Ensure the 'default' docker context exists since manifests normalize empty context to 'default'
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

	// Prepare temp workdir from scenario
	tempDir := t.TempDir()
	if err := copyTree(filepath.Join("testdata", "scenarios", "sops"), tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}

	// Generate an age key and write to file in scenario directory
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate age identity: %v", err)
	}
	keyPath := filepath.Join(tempDir, "age.key")
	keyContent := fmt.Sprintf("# created by test\n# public key: %s\n%s\n", id.Recipient(), id.String())
	if err := os.WriteFile(keyPath, []byte(keyContent), 0o600); err != nil {
		t.Fatalf("write age key: %v", err)
	}

	// Build dockform
	bin := buildDockform(t)

	// Always cleanup labeled resources
	t.Cleanup(func() { cleanupByLabel(t, identifier) })

	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	// Create an encrypted secret via CLI
	out := runCmd(t, tempDir, env, bin, "secrets", "create", filepath.Join("secrets", "app.env"))
	if !strings.Contains(out, "secret created:") {
		t.Fatalf("unexpected secret create output:\n%s", out)
	}
	// Ensure file appears encrypted (contains ENC[)
	b, err := os.ReadFile(filepath.Join(tempDir, "secrets", "app.env"))
	if err != nil {
		t.Fatalf("read secret file: %v", err)
	}
	if !strings.Contains(string(b), "ENC[") {
		t.Fatalf("expected encrypted content, got:\n%s", string(b))
	}

	// Apply: should decrypt secret and pass as inline env so compose sees SECRET_KEY
	_ = runCmdWithStdin(t, tempDir, env, bin, "yes\n", "apply", "--manifest", tempDir)

	// Find running container by label and verify the secret value is readable in the container
	names := dockerLines(t, ctx, "ps", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(names) == 0 {
		t.Fatalf("expected running container")
	}
	name := names[0]
	// The create command seeds SECRET_KEY=secret
	out2 := runDocker(t, "exec", name, "cat", "/tmp/secret")
	if strings.TrimSpace(out2) != "secret" {
		t.Fatalf("unexpected secret value in container: %q", out2)
	}
}
