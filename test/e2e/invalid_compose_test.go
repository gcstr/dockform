package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidComposePlanAndApply(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}

	// Prepare temp workdir by copying scenario with invalid compose
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "invalid_compose")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}

	// Build dockform once and reuse path
	bin := buildDockform(t)

	env := append(os.Environ(), "DOCKFORM_RUN_ID=invalid")

	// PLAN should fail with External error (exit code 70) and friendly message
	out, code := runCmdExpectError(t, tempDir, env, bin, "plan", "-c", tempDir)
	if code != 70 {
		t.Fatalf("expected exit code 70 for invalid compose on plan, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "invalid compose file") || !strings.Contains(out, "for application app") {
		t.Fatalf("expected invalid compose error message, got:\n%s", out)
	}

	// APPLY should also fail similarly (feed confirmation)
	out2, code2 := runCmdExpectErrorWithStdin(t, tempDir, env, bin, "yes\n", "apply", "-c", tempDir)
	if code2 != 70 {
		t.Fatalf("expected exit code 70 for invalid compose on apply, got %d\n%s", code2, out2)
	}
	if !strings.Contains(out2, "invalid compose file") || !strings.Contains(out2, "for application app") {
		t.Fatalf("expected invalid compose error message, got:\n%s", out2)
	}
}

func runCmdExpectError(t *testing.T, dir string, env []string, name string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command to fail, but it succeeded: %s %v\n%s", name, args, string(out))
	}
	code := 1
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return string(out), code
}

func runCmdExpectErrorWithStdin(t *testing.T, dir string, env []string, name string, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command to fail, but it succeeded: %s %v\n%s", name, args, string(out))
	}
	code := 1
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return string(out), code
}
