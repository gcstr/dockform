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
	if !strings.Contains(plain, "volume df_e2e_"+runID+"_vol will be created") {
		t.Fatalf("plan missing add volume:\n%s", out)
	}
	if !strings.Contains(plain, "network df_e2e_"+runID+"_net will be created") {
		t.Fatalf("plan missing add network:\n%s", out)
	}
	// Service detection may vary per compose; require at least one application/service hint
	if !(strings.Contains(out, "service app/hello") || strings.Contains(out, "application app")) {
		t.Fatalf("plan missing application/service lines:\n%s", out)
	}

	// APPLY (verbose + overlay debug to surface compose errors clearly)
	envApply := append([]string{}, env...)
	envApply = append(envApply, "DOCKFORM_DEBUG_OVERLAY=1")
	_ = runCmd(t, tempDir, envApply, bin, "-v", "apply", "-c", tempDir)

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
	if !(strings.Contains(out2, "[noop] service app/hello up-to-date") || strings.Contains(out2, "[noop] service app/hello running")) {
		if strings.Contains(out2, "[add] service app/hello will be started") {
			t.Fatalf("service should not require start after apply, got plan:\n%s", out2)
		}
	}
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
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer out.Close()
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
