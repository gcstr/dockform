package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestComposeNetwork_NotOrphanedOnRepeatApply reproduces GH #54: a network defined
// only in a compose file (not in dockform.yml) is created on first apply, then
// must NOT be reported as "will be deleted" on subsequent plans/applies, and must
// still exist afterwards.
func TestComposeNetwork_NotOrphanedOnRepeatApply(t *testing.T) {
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

	netName := "df_e2e_" + runID + "_net"
	bin := buildDockform(t)

	t.Cleanup(func() {
		cleanupByLabel(t, identifier)
	})

	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "compose_network")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}

	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	// 1. FIRST APPLY: compose creates the network
	if out, stderr, code := runCmdDetailed(t, tempDir, env, bin, "apply", "--skip-confirmation", "--manifest", tempDir); code != 0 {
		t.Fatalf("first apply failed (code %d)\nSTDOUT:\n%s\nSTDERR:\n%s", code, out, stderr)
	}

	nets := dockerLines(t, ctx, "network", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	if !contains(nets, netName) {
		t.Fatalf("expected compose-defined network %q to exist after first apply, got %v", netName, nets)
	}

	// 2. SECOND PLAN: the compose network must not be flagged for deletion
	planOut := runCmd(t, tempDir, env, bin, "plan", "--manifest", tempDir)
	if strings.Contains(planOut, netName+" will be deleted") {
		t.Fatalf("compose-defined network falsely marked for deletion on repeat plan:\n%s", planOut)
	}

	// 3. SECOND APPLY: must not destroy the network
	if out, stderr, code := runCmdDetailed(t, tempDir, env, bin, "apply", "--skip-confirmation", "--manifest", tempDir); code != 0 {
		t.Fatalf("second apply failed (code %d)\nSTDOUT:\n%s\nSTDERR:\n%s", code, out, stderr)
	}

	// 4. VERIFY: network still exists
	netsAfter := dockerLines(t, ctx, "network", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	if !contains(netsAfter, netName) {
		t.Fatalf("expected compose-defined network %q to survive repeat apply, got %v", netName, netsAfter)
	}
}
