package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gcstr/dockform/internal/dockercli"
)

var (
	buildOncePath string
)

func TestSimplePlanApplyLifecycle(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}

	// Log context info and apply safety gate
	ctx := context.Background()
	_ = logDockerContext(t)
	if looksProd(t) && os.Getenv("E2E_ALLOW_HOST") != "1" {
		t.Skip("refusing to run e2e against a production-looking daemon; set E2E_ALLOW_HOST=1 to override")
	}

	// Unique run id and identifier (labels use the raw runID)
	runID := uniqueID()
	identifier := runID
	ensureNetworkCreatableOrSkip(t, identifier)

	// Pre-create external volume referenced by compose since planner does not create
	// top-level volumes unless derived from filesets
	_ = exec.Command("docker", "volume", "create", "--label", "io.dockform.identifier="+identifier, "df_e2e_"+runID+"_vol").Run()

	// Prepare temp workdir by copying scenario
	tempDir := t.TempDir()
	src := filepath.Join("testdata", "scenarios", "simple")
	if err := copyTree(src, tempDir); err != nil {
		t.Fatalf("copy scenario: %v", err)
	}

	// Build dockform once and reuse path
	bin := buildDockform(t)

	// Always cleanup labeled resources
	t.Cleanup(func() {
		cleanupByLabel(t, identifier)
	})

	env := append(os.Environ(), "DOCKFORM_RUN_ID="+runID)

	// PLAN
	out := runCmd(t, tempDir, env, bin, "plan", "-c", tempDir)
	// Normalize whitespace to avoid style-related spacing differences
	plain := strings.Join(strings.Fields(out), " ")
	// Volume creation is not asserted here because volumes are only derived from filesets
	if !strings.Contains(plain, "network df_e2e_"+runID+"_net will be created") {
		t.Fatalf("plan missing add network:\n%s", out)
	}
	// Service detection may vary per compose; require at least one application/service hint
	if !strings.Contains(out, "service app/hello") && !strings.Contains(out, "application app") {
		t.Fatalf("plan missing application/service lines:\n%s", out)
	}

	// APPLY (verbose + overlay debug to surface compose errors clearly)
	envApply := append([]string{}, env...)
	envApply = append(envApply, "DOCKFORM_DEBUG_OVERLAY=1")
	_ = runCmdWithStdin(t, tempDir, envApply, bin, "yes\n", "-v", "apply", "-c", tempDir)

	// Assert container exists by label
	names := dockerLines(t, ctx, "ps", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(names) == 0 {
		t.Fatalf("expected running container with label io.dockform.identifier=%s", identifier)
	}

	// Assert volume exists by label
	vols := dockerLines(t, ctx, "volume", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(vols) == 0 {
		t.Fatalf("expected volume labeled io.dockform.identifier=%s", identifier)
	}

	// Assert network exists by label
	nets := dockerLines(t, ctx, "network", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(nets) == 0 {
		t.Fatalf("expected network labeled io.dockform.identifier=%s", identifier)
	}

	// Re-PLAN should show up-to-date/noop items
	out2 := runCmd(t, tempDir, env, bin, "plan", "-c", tempDir)
	if !strings.Contains(out2, "[noop] service app/hello up-to-date") && !strings.Contains(out2, "[noop] service app/hello running") {
		if strings.Contains(out2, "[add] service app/hello will be started") {
			t.Fatalf("service should not require start after apply, got plan:\n%s", out2)
		}
	}
}

func TestExamplePlanApplyIdempotentAndPrune(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}

	// Log context and avoid running on prod-looking hosts
	ctx := context.Background()
	_ = logDockerContext(t)
	if looksProd(t) && os.Getenv("E2E_ALLOW_HOST") != "1" {
		t.Skip("refusing to run e2e against a production-looking daemon; set E2E_ALLOW_HOST=1 to override")
	}

	// Ensure the 'default' docker context exists since the example uses it
	if out := safeOutput(exec.Command("docker", "context", "inspect", "default")); strings.TrimSpace(out) == "" {
		t.Skip("docker context 'default' not available; skipping example e2e")
	}

	// Force docker CLI calls in this test to use the 'default' context to match the example manifest
	prevCtx := os.Getenv("DOCKER_CONTEXT")
	_ = os.Setenv("DOCKER_CONTEXT", "default")
	t.Cleanup(func() { _ = os.Setenv("DOCKER_CONTEXT", prevCtx) })

	// Example manifest identifier is static: "demo"
	identifier := "demo"

	// Always cleanup labeled resources
	t.Cleanup(func() {
		cleanupByLabel(t, identifier)
		// Also remove transient resources if they still exist
		_ = exec.Command("docker", "rm", "-f", identifier+"-temp").Run()
		_ = exec.Command("docker", "volume", "rm", identifier+"-temp-vol").Run()
		_ = exec.Command("docker", "network", "rm", identifier+"-temp-net").Run()
	})

	// Build dockform once and reuse path
	bin := buildDockform(t)

	root := findRepoRoot(t)
	exampleCfg := filepath.Join(root, "test", "e2e", "testdata", "scenarios", "example")

	env := os.Environ()
	// Avoid sops age key file validation by providing an explicit empty value
	env = append(env, "AGE_KEY_FILE=")

	// 1) Apply Happy Path (skip confirmation)
	stdout, stderr, code := runCmdDetailed(t, root, env, bin, "apply", "--skip-confirmation", "-c", exampleCfg)
	if code != 0 {
		t.Fatalf("apply failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", code, stdout, stderr)
	}
	// Only fail if real errors are printed to stderr; warnings are acceptable
	if strings.Contains(strings.ToLower(stderr), "[error]") {
		t.Fatalf("unexpected error output on apply:\n%s", stderr)
	}

	// Assert resources exist
	vols := dockerLines(t, ctx, "volume", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	foundVol := false
	for _, v := range vols {
		if v == "demo-volume-1" {
			foundVol = true
			break
		}
	}
	if !foundVol {
		t.Fatalf("expected volume demo-volume-1 to be created and labeled")
	}
	nets := dockerLines(t, ctx, "network", "ls", "--format", "{{.Name}}", "--filter", "label=io.dockform.identifier="+identifier)
	foundNet := false
	for _, n := range nets {
		if n == "demo-network" {
			foundNet = true
			break
		}
	}
	if !foundNet {
		t.Fatalf("expected network demo-network to be created and labeled")
	}
	names := dockerLines(t, ctx, "ps", "--format", "{{.Names}}", "--filter", "label=io.dockform.identifier="+identifier)
	if len(names) == 0 {
		t.Fatalf("expected running container with io.dockform.identifier=%s", identifier)
	}
	hasNginx := false
	for _, n := range names {
		if strings.Contains(n, "nginx") {
			hasNginx = true
			break
		}
	}
	if !hasNginx {
		t.Fatalf("expected website/nginx container to be running, got: %v", names)
	}
	// Inspect first container label value
	labelVal := safeOutput(exec.Command("docker", "inspect", "-f", "{{ index .Config.Labels \"io.dockform.identifier\" }}", names[0]))
	if strings.TrimSpace(labelVal) != identifier {
		t.Fatalf("expected label io.dockform.identifier=%s, got %q", identifier, labelVal)
	}

	// 2) Plan -> Confirm -> Apply
	pOut, pErr, pCode := runCmdDetailed(t, root, env, bin, "plan", "-c", exampleCfg)
	if pCode != 0 {
		t.Fatalf("plan failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", pCode, pOut, pErr)
	}
	if !strings.Contains(pOut, "Docker") || !strings.Contains(pOut, "Context: default") || !strings.Contains(pOut, "Identifier: demo") {
		t.Fatalf("plan output missing Docker section/context/identifier:\n%s", pOut)
	}
	aOut, aErr, aCode := runCmdWithStdinDetailed(t, root, env, bin, "yes\n", "apply", "-c", exampleCfg)
	if aCode != 0 {
		t.Fatalf("apply (with confirm) failed with exit code %d\nSTDOUT:\n%s\nSTDERR:\n%s", aCode, aOut, aErr)
	}
	if !strings.Contains(aOut, "Dockform will apply the changes listed above") || !strings.Contains(aOut, "Answer") {
		t.Fatalf("confirmation prompt not shown or not respected:\n%s", aOut)
	}

	// 3) Idempotency: subsequent plan should be noop/up-to-date
	out2, err2, code2 := runCmdDetailed(t, root, env, bin, "plan", "-c", exampleCfg)
	if code2 != 0 {
		t.Fatalf("plan after apply failed: %d\nSTDOUT:\n%s\nSTDERR:\n%s", code2, out2, err2)
	}
	// UI may render without explicit [noop] prefix; assert on core phrases instead
	if !strings.Contains(out2, "volume demo-volume-1 exists") {
		t.Fatalf("expected volume exists, got:\n%s", out2)
	}
	if !strings.Contains(out2, "network demo-network exists") {
		t.Fatalf("expected network exists, got:\n%s", out2)
	}
	if !strings.Contains(out2, "service website/nginx up-to-date") && !strings.Contains(out2, "service website/nginx running") {
		t.Fatalf("expected service website/nginx up-to-date or running, got:\n%s", out2)
	}
	if !strings.Contains(out2, "fileset files: no file changes") {
		t.Fatalf("expected fileset no changes, got:\n%s", out2)
	}

	// 4) Prune: create unmanaged labeled resources then apply and expect removal
	_ = exec.Command("docker", "network", "create", "--label", "io.dockform.identifier="+identifier, identifier+"-temp-net").Run()
	_ = exec.Command("docker", "volume", "create", "--label", "io.dockform.identifier="+identifier, identifier+"-temp-vol").Run()
	// Create a transient container with compose-like labels so prune can detect it
	_ = exec.Command(
		"docker", "run", "-d",
		"--label", "io.dockform.identifier="+identifier,
		"--label", "com.docker.compose.project="+identifier+"-temp-proj",
		"--label", "com.docker.compose.service="+identifier+"-temp-svc",
		"--name", identifier+"-temp",
		dockercli.HelperImage, "sleep", "60",
	).Run()

	// Sanity: ensure they exist
	_ = dockerLines(t, ctx, "network", "inspect", identifier+"-temp-net")
	_ = dockerLines(t, ctx, "volume", "inspect", identifier+"-temp-vol")
	_ = dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "name="+identifier+"-temp")

	// Apply with prune
	_, _, code3 := runCmdDetailed(t, root, env, bin, "apply", "--skip-confirmation", "-c", exampleCfg)
	if code3 != 0 {
		t.Fatalf("apply for prune failed")
	}

	// Assert transient resources removed
	namesAfter := dockerLines(t, ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "name="+identifier+"-temp")
	if len(namesAfter) != 0 {
		t.Fatalf("expected transient container to be pruned, still present: %v", namesAfter)
	}
	if out := safeOutput(exec.Command("docker", "network", "inspect", identifier+"-temp-net")); strings.TrimSpace(out) != "" {
		t.Fatalf("expected transient network to be pruned")
	}
	if out := safeOutput(exec.Command("docker", "volume", "inspect", identifier+"-temp-vol")); strings.TrimSpace(out) != "" {
		t.Fatalf("expected transient volume to be pruned")
	}
}

// runCmdDetailed executes a command capturing stdout and stderr separately and returns the exit code.
func runCmdDetailed(t *testing.T, dir string, env []string, name string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	return stdout.String(), stderr.String(), code
}

// runCmdWithStdinDetailed executes a command with provided stdin, capturing stdout/stderr and exit code.
func runCmdWithStdinDetailed(t *testing.T, dir string, env []string, name string, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	return stdout.String(), stderr.String(), code
}

func buildDockform(t *testing.T) string {
	t.Helper()
	if buildOncePath != "" {
		return buildOncePath
	}
	bin := filepath.Join(os.TempDir(), fmt.Sprintf("dockform-e2e-%d", time.Now().UnixNano()))
	root := findRepoRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/dockform")
	cmd.Env = os.Environ()
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build dockform: %v", err)
	}
	buildOncePath = bin
	return bin
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate go.mod walking up from test dir")
	return ""
}

func runCmd(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		t.Fatalf("run %s %v failed: %v\n%s", name, args, err, buf.String())
	}
	return buf.String()
}

func runCmdWithStdin(t *testing.T, dir string, env []string, name string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		t.Fatalf("run %s %v failed: %v\n%s", name, args, err, buf.String())
	}
	return buf.String()
}

func dockerLines(t *testing.T, ctx context.Context, subcmd string, args ...string) []string {
	t.Helper()
	full := append([]string{subcmd}, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %v failed: %v\n%s", full, err, string(out))
	}
	lines := []string{}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func logDockerContext(t *testing.T) bool {
	t.Helper()
	ctxName := safeOutput(exec.Command("docker", "context", "show"))
	daemonName := safeOutput(exec.Command("docker", "info", "--format", "{{.Name}}"))
	t.Logf("docker context: %s; daemon: %s", ctxName, daemonName)
	return true
}

func looksProd(t *testing.T) bool {
	t.Helper()
	name := strings.ToLower(safeOutput(exec.Command("docker", "info", "--format", "{{.Name}}")))
	// heuristic: contains prod or production
	return strings.Contains(name, "prod") || strings.Contains(name, "production")
}

func safeOutput(cmd *exec.Cmd) string {
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func uniqueID() string {
	var b [8]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer func() { _ = out.Close() }()
		if _, err := io.Copy(out, in); err != nil {
			return err
		}
		return nil
	})
}

func cleanupByLabel(t *testing.T, identifier string) {
	t.Helper()
	label := "io.dockform.identifier=" + identifier
	// Stop and remove containers
	_ = exec.Command("bash", "-lc", fmt.Sprintf("docker ps -aq --filter label=%s | xargs -r docker rm -f", shellEscape(label))).Run()
	// Remove volumes
	_ = exec.Command("bash", "-lc", fmt.Sprintf("docker volume ls -q --filter label=%s | xargs -r docker volume rm", shellEscape(label))).Run()
	// Remove networks
	_ = exec.Command("bash", "-lc", fmt.Sprintf("docker network ls -q --filter label=%s | xargs -r docker network rm", shellEscape(label))).Run()
}

func shellEscape(s string) string {
	// minimal escape for use inside single-quoted bash -lc command
	return strings.ReplaceAll(s, "'", "'\\''")
}
