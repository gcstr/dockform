package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDestroy_FullLifecycle(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}

	// Log context and apply safety gate
	ctx := context.Background()
	_ = logDockerContext(t)
	if looksProd(t) && os.Getenv("E2E_ALLOW_HOST") != "1" {
		t.Skip("refusing to run e2e against a production-looking daemon; set E2E_ALLOW_HOST=1 to override")
	}

	// Unique run id and identifier
	runID := uniqueID()
	identifier := runID
	ensureNetworkCreatableOrSkip(t, identifier)

	// Pre-create external volume referenced by compose since planner does not create
	// top-level volumes unless derived from filesets
	_ = exec.Command("docker", "volume", "create", "--label", "io.dockform.identifier="+identifier, "df_e2e_"+runID+"_vol").Run()

	// Build dockform once and reuse path
	bin := buildDockform(t)

	// Always cleanup labeled resources (redundant with destroy but ensures clean state)
	t.Cleanup(func() {
		cleanupByLabel(t, identifier)
	})

	// Prepare temp workdir by copying scenario
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "simple")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}

	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	// 1. APPLY: Create resources to destroy later
	stdout, stderr, code := runCmdWithStdinDetailed(t, tempDir, env, bin, "yes\n", "apply", "--manifest", tempDir)
	if code != 0 {
		t.Fatalf("apply failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}

	// Assert resources exist before destroy
	containers := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	networks := dockerLines(t, ctx, "network", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	volumes := dockerLines(t, ctx, "volume", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)

	if len(containers) == 0 {
		t.Fatalf("expected containers to be created before destroy")
	}
	if len(networks) == 0 {
		t.Fatalf("expected networks to be created before destroy")
	}
	if len(volumes) == 0 {
		t.Fatalf("expected volumes to be created before destroy")
	}
	t.Logf("Found %d containers, %d networks, %d volumes before destroy", len(containers), len(networks), len(volumes))

	// 2. DESTROY WITH CONFIRMATION: Test the complete destroy flow
	destroyOut, destroyErr, destroyCode := runCmdWithStdinDetailed(t, tempDir, env, bin, identifier+"\n", "destroy", "--manifest", tempDir)
	if destroyCode != 0 {
		t.Fatalf("destroy failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", destroyCode, destroyOut, destroyErr)
	}

	// Verify plan was shown (sections and actions)
	if !strings.Contains(destroyOut, "Stacks") {
		t.Fatalf("destroy output missing Stacks section:\n%s", destroyOut)
	}
	if !strings.Contains(destroyOut, "Networks") {
		t.Fatalf("destroy output missing Networks section:\n%s", destroyOut)
	}
	if !strings.Contains(destroyOut, "Volumes") {
		t.Fatalf("destroy output missing Volumes section:\n%s", destroyOut)
	}
	if !strings.Contains(destroyOut, "will be deleted") {
		t.Fatalf("destroy output missing delete actions:\n%s", destroyOut)
	}

	// Verify confirmation prompt was shown
	if !strings.Contains(destroyOut, "This will destroy ALL managed resources with identifier '"+identifier+"'") {
		t.Fatalf("destroy output missing confirmation prompt:\n%s", destroyOut)
	}
	if !strings.Contains(destroyOut, "Type the identifier name '"+identifier+"' to confirm") {
		t.Fatalf("destroy output missing identifier confirmation:\n%s", destroyOut)
	}

	// 3. VERIFY RESOURCES DESTROYED: Assert all labeled resources are gone
	containersAfter := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	networksAfter := dockerLines(t, ctx, "network", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	volumesAfter := dockerLines(t, ctx, "volume", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)

	if len(containersAfter) != 0 {
		t.Fatalf("expected all containers to be destroyed, but found: %v", containersAfter)
	}
	if len(networksAfter) != 0 {
		t.Fatalf("expected all networks to be destroyed, but found: %v", networksAfter)
	}
	if len(volumesAfter) != 0 {
		t.Fatalf("expected all volumes to be destroyed, but found: %v", volumesAfter)
	}

	t.Logf("Successfully destroyed all resources: %d containers, %d networks, %d volumes", len(containers), len(networks), len(volumes))

	// 4. DESTROY AGAIN: Should show no resources to destroy
	destroyOut2, destroyErr2, destroyCode2 := runCmdDetailed(t, tempDir, env, bin, "destroy", "--manifest", tempDir)
	if destroyCode2 != 0 {
		t.Fatalf("second destroy failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", destroyCode2, destroyOut2, destroyErr2)
	}

	if !strings.Contains(destroyOut2, "No managed resources found to destroy.") {
		t.Fatalf("expected no resources message on second destroy:\n%s", destroyOut2)
	}
}

func TestDestroy_WrongIdentifier_CancelsDestruction(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}

	// Log context and apply safety gate
	ctx := context.Background()
	_ = logDockerContext(t)
	if looksProd(t) && os.Getenv("E2E_ALLOW_HOST") != "1" {
		t.Skip("refusing to run e2e against a production-looking daemon; set E2E_ALLOW_HOST=1 to override")
	}

	// Unique run id and identifier
	runID := uniqueID()
	identifier := runID
	ensureNetworkCreatableOrSkip(t, identifier)

	// Pre-create external volume referenced by compose since planner does not create
	// top-level volumes unless derived from filesets
	_ = exec.Command("docker", "volume", "create", "--label", "io.dockform.identifier="+identifier, "df_e2e_"+runID+"_vol").Run()

	// Build dockform once and reuse path
	bin := buildDockform(t)

	// Always cleanup labeled resources
	t.Cleanup(func() {
		cleanupByLabel(t, identifier)
	})

	// Prepare temp workdir by copying scenario
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "simple")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}

	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	// 1. APPLY: Create resources
	stdout, stderr, code := runCmdWithStdinDetailed(t, tempDir, env, bin, "yes\n", "apply", "--manifest", tempDir)
	if code != 0 {
		t.Fatalf("apply failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}

	// Assert resources exist
	containers := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(containers) == 0 {
		t.Fatalf("expected containers to be created before destroy")
	}

	// 2. DESTROY WITH WRONG IDENTIFIER: Should cancel
	destroyOut, destroyErr, destroyCode := runCmdWithStdinDetailed(t, tempDir, env, bin, "wrong-identifier\n", "destroy", "--manifest", tempDir)
	if destroyCode != 0 {
		t.Fatalf("destroy failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", destroyCode, destroyOut, destroyErr)
	}

	// Verify destruction was canceled
	if !strings.Contains(destroyOut, " canceled") {
		t.Fatalf("expected destruction to be canceled:\n%s", destroyOut)
	}

	// 3. VERIFY RESOURCES STILL EXIST: Nothing should be destroyed
	containersAfter := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(containersAfter) != len(containers) {
		t.Fatalf("expected resources to still exist after canceled destroy, had %d containers, now have %d", len(containers), len(containersAfter))
	}
}

func TestDestroy_SkipConfirmation_DestroyImmediately(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}

	// Log context and apply safety gate
	ctx := context.Background()
	_ = logDockerContext(t)
	if looksProd(t) && os.Getenv("E2E_ALLOW_HOST") != "1" {
		t.Skip("refusing to run e2e against a production-looking daemon; set E2E_ALLOW_HOST=1 to override")
	}

	// Unique run id and identifier
	runID := uniqueID()
	identifier := runID
	ensureNetworkCreatableOrSkip(t, identifier)

	// Pre-create external volume referenced by compose since planner does not create
	// top-level volumes unless derived from filesets
	_ = exec.Command("docker", "volume", "create", "--label", "io.dockform.identifier="+identifier, "df_e2e_"+runID+"_vol").Run()

	// Build dockform once and reuse path
	bin := buildDockform(t)

	// Always cleanup labeled resources
	t.Cleanup(func() {
		cleanupByLabel(t, identifier)
	})

	// Prepare temp workdir by copying scenario
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "simple")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}

	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	// 1. APPLY: Create resources
	stdout, stderr, code := runCmdWithStdinDetailed(t, tempDir, env, bin, "yes\n", "apply", "--manifest", tempDir)
	if code != 0 {
		t.Fatalf("apply failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}

	// Assert resources exist
	containers := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	networks := dockerLines(t, ctx, "network", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	volumes := dockerLines(t, ctx, "volume", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)

	if len(containers) == 0 {
		t.Fatalf("expected containers to be created before destroy")
	}

	// 2. DESTROY WITH SKIP CONFIRMATION: Should proceed immediately
	destroyOut, destroyErr, destroyCode := runCmdDetailed(t, tempDir, env, bin, "destroy", "--skip-confirmation", "--manifest", tempDir)
	if destroyCode != 0 {
		t.Fatalf("destroy --skip-confirmation failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", destroyCode, destroyOut, destroyErr)
	}

	// Verify no confirmation prompt was shown
	if strings.Contains(destroyOut, "Type the identifier name") {
		t.Fatalf("did not expect confirmation prompt with --skip-confirmation:\n%s", destroyOut)
	}
	if strings.Contains(destroyOut, " canceled") {
		t.Fatalf("did not expect destruction to be canceled with --skip-confirmation:\n%s", destroyOut)
	}

	// 3. VERIFY RESOURCES DESTROYED: Assert all labeled resources are gone
	containersAfter := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	networksAfter := dockerLines(t, ctx, "network", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	volumesAfter := dockerLines(t, ctx, "volume", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)

	if len(containersAfter) != 0 {
		t.Fatalf("expected all containers to be destroyed, but found: %v", containersAfter)
	}
	if len(networksAfter) != 0 {
		t.Fatalf("expected all networks to be destroyed, but found: %v", networksAfter)
	}
	if len(volumesAfter) != 0 {
		t.Fatalf("expected all volumes to be destroyed, but found: %v", volumesAfter)
	}

	t.Logf("Successfully destroyed all resources with --skip-confirmation: %d containers, %d networks, %d volumes", len(containers), len(networks), len(volumes))
}

func TestDestroy_IndependentOfConfigFile(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}

	// Log context and apply safety gate
	ctx := context.Background()
	_ = logDockerContext(t)
	if looksProd(t) && os.Getenv("E2E_ALLOW_HOST") != "1" {
		t.Skip("refusing to run e2e against a production-looking daemon; set E2E_ALLOW_HOST=1 to override")
	}

	// Unique run id and identifier
	runID := uniqueID()
	identifier := runID
	ensureNetworkCreatableOrSkip(t, identifier)

	// Pre-create external volume referenced by compose since planner does not create
	// top-level volumes unless derived from filesets
	_ = exec.Command("docker", "volume", "create", "--label", "io.dockform.identifier="+identifier, "df_e2e_"+runID+"_vol").Run()

	// Build dockform once and reuse path
	bin := buildDockform(t)

	// Always cleanup labeled resources
	t.Cleanup(func() {
		cleanupByLabel(t, identifier)
	})

	// Prepare temp workdir by copying scenario
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "simple")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}

	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	// 1. APPLY: Create resources
	stdout, stderr, code := runCmdWithStdinDetailed(t, tempDir, env, bin, "yes\n", "apply", "--manifest", tempDir)
	if code != 0 {
		t.Fatalf("apply failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}

	// Assert resources exist
	containers := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(containers) == 0 {
		t.Fatalf("expected containers to be created before destroy")
	}

	// 2. CREATE A DIFFERENT CONFIG FILE: Test that destroy ignores config content
	modifiedTempDir := t.TempDir()
	src2 := filepath.Join("testdata", "scenarios", "example") // Different scenario
	if err := copyTree(src2, modifiedTempDir); err != nil {
		t.Fatalf("copy different scenario: %v", err)
	}

	// 3. DESTROY WITH DIFFERENT CONFIG: Should still find and destroy original resources
	destroyOut, destroyErr, destroyCode := runCmdWithStdinDetailed(t, modifiedTempDir, env, bin, identifier+"\n", "destroy", "--manifest", modifiedTempDir)
	if destroyCode != 0 {
		t.Fatalf("destroy with different config failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", destroyCode, destroyOut, destroyErr)
	}

	// Verify resources were still discovered and destroyed despite different config
	if !strings.Contains(destroyOut, "Volumes") || !strings.Contains(destroyOut, "Networks") || !strings.Contains(destroyOut, "Stacks") {
		t.Fatalf("destroy should have found resources despite different config:\n%s", destroyOut)
	}

	// 4. VERIFY RESOURCES DESTROYED: Assert all labeled resources are gone
	containersAfter := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(containersAfter) != 0 {
		t.Fatalf("expected all containers to be destroyed regardless of config file, but found: %v", containersAfter)
	}

	t.Logf("Successfully destroyed resources independent of config file content")
}

// TestDestroy_ScopedToStack verifies that `destroy --stack <context/stack>` only
// removes the targeted stack's services, leaving other stacks, the shared
// context-level network, and non-fileset volumes intact. Regression test for
// GH #55, where a scoped destroy tore down ALL managed resources.
func TestDestroy_ScopedToStack(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}

	// Log context and apply safety gate
	ctx := context.Background()
	_ = logDockerContext(t)
	if looksProd(t) && os.Getenv("E2E_ALLOW_HOST") != "1" {
		t.Skip("refusing to run e2e against a production-looking daemon; set E2E_ALLOW_HOST=1 to override")
	}

	// Unique run id and identifier
	runID := uniqueID()
	identifier := runID
	ensureNetworkCreatableOrSkip(t, identifier)

	app1Project := "df_e2e_" + runID + "_app1"
	app2Project := "df_e2e_" + runID + "_app2"

	// Pre-create external volumes referenced by each stack's compose since the
	// planner does not create top-level external volumes.
	_ = exec.Command("docker", "volume", "create", "--label", "io.dockform.identifier="+identifier, "df_e2e_"+runID+"_vol1").Run()
	_ = exec.Command("docker", "volume", "create", "--label", "io.dockform.identifier="+identifier, "df_e2e_"+runID+"_vol2").Run()

	// Build dockform once and reuse path
	bin := buildDockform(t)

	// Always cleanup labeled resources
	t.Cleanup(func() {
		cleanupByLabel(t, identifier)
	})

	// Prepare temp workdir by copying scenario
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "multi_stack")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}

	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	// 1. APPLY: Create both stacks, the shared network, and both volumes
	stdout, stderr, code := runCmdWithStdinDetailed(t, tempDir, env, bin, "yes\n", "apply", "--manifest", tempDir)
	if code != 0 {
		t.Fatalf("apply failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}

	// Assert both stacks' containers exist before destroy
	app1Before := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=com.docker.compose.project="+app1Project)
	app2Before := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=com.docker.compose.project="+app2Project)
	if len(app1Before) == 0 || len(app2Before) == 0 {
		t.Fatalf("expected both stacks to have containers before destroy: app1=%v app2=%v", app1Before, app2Before)
	}

	// 2. DESTROY SCOPED TO app1 ONLY
	destroyOut, destroyErr, destroyCode := runCmdWithStdinDetailed(t, tempDir, env, bin, identifier+"\n", "destroy", "--stack", "default/app1", "--manifest", tempDir)
	if destroyCode != 0 {
		t.Fatalf("scoped destroy failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", destroyCode, destroyOut, destroyErr)
	}

	// Confirmation copy should reflect the scoped (targeted) destroy, not the
	// full "ALL managed resources" warning.
	if !strings.Contains(destroyOut, "targeted resources shown above") {
		t.Fatalf("scoped destroy should show targeted confirmation copy:\n%s", destroyOut)
	}
	if strings.Contains(destroyOut, "destroy ALL managed resources") {
		t.Fatalf("scoped destroy must not show the ALL-resources warning:\n%s", destroyOut)
	}

	// 3. VERIFY ONLY app1 WAS DESTROYED
	app1After := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=com.docker.compose.project="+app1Project)
	app2After := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=com.docker.compose.project="+app2Project)
	if len(app1After) != 0 {
		t.Fatalf("expected app1 containers to be destroyed, but found: %v", app1After)
	}
	if len(app2After) == 0 {
		t.Fatalf("expected app2 containers to survive scoped destroy, but none found")
	}

	// Shared context-level network must be preserved.
	networksAfter := dockerLines(t, ctx, "network", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(networksAfter) == 0 {
		t.Fatalf("expected shared network to survive scoped destroy, but none found")
	}

	// Non-fileset volumes (both stacks') must be preserved.
	volumesAfter := dockerLines(t, ctx, "volume", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(volumesAfter) != 2 {
		t.Fatalf("expected both volumes to survive scoped destroy, found: %v", volumesAfter)
	}

	t.Logf("Scoped destroy removed only app1; app2, shared network, and volumes preserved")
}
