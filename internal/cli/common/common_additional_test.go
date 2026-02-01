package common

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/gcstr/dockform/internal/cli/clitest"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

type capturePrinter struct {
	warns []string
}

func (c *capturePrinter) Plain(string, ...any) {}
func (c *capturePrinter) Info(string, ...any)  {}
func (c *capturePrinter) Warn(format string, a ...any) {
	c.warns = append(c.warns, fmt.Sprintf(format, a...))
}
func (c *capturePrinter) Error(string, ...any) {}

func openTTYOrSkip(t *testing.T) (*os.File, *os.File) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("unable to open pty: %v", err)
	}
	return master, slave
}

func drainTTY(t *testing.T, r *os.File) {
	go func() {
		_, _ = io.Copy(io.Discard, r)
	}()
}

func writeManifest(t *testing.T, dir string, content string) string {
	t.Helper()
	if err := os.WriteFile(dir, []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

func createSampleManifest(t *testing.T) (string, string) {
	root := t.TempDir()
	stackDir := filepath.Join(root, "stack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatalf("mkdir stack: %v", err)
	}
	dc := filepath.Join(stackDir, "docker-compose.yml")
	if err := os.WriteFile(dc, []byte("version: '3'\nservices: {}\n"), 0o644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	manifestPath := filepath.Join(root, "dockform.yml")
	content := strings.Join([]string{
		"identifier: demo",
		"contexts:",
		"  default: {}",
		"stacks:",
		"  default/app:",
		"    root: stack",
		"    files:",
		"      - docker-compose.yml",
		"    environment:",
		"      inline:",
		"        - API_KEY=${API_KEY}",
	}, "\n") + "\n"
	writeManifest(t, manifestPath, content)
	return manifestPath, root
}

func TestLoadConfigWithWarningsEmitsMessages(t *testing.T) {
	path, _ := createSampleManifest(t)

	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "")
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("config", path); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	pr := &capturePrinter{}
	cfg, err := LoadConfigWithWarnings(cmd, pr)
	if err != nil {
		t.Fatalf("LoadConfigWithWarnings: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected config")
	}
	if len(pr.warns) == 0 {
		t.Fatalf("expected warning for missing env variable")
	}
	if !strings.Contains(pr.warns[0], "API_KEY") {
		t.Fatalf("expected warning to mention API_KEY, got %q", pr.warns[0])
	}
}

func TestLoadConfigWithWarningsInteractiveSelection(t *testing.T) {
	// Check PTY support FIRST before any setup to avoid Windows cleanup issues
	master, slave := openTTYOrSkip(t)
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Fatalf("close master pty: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Fatalf("close slave pty: %v", err)
		}
	})
	drainTTY(t, master)

	// Now do the test setup
	temp := t.TempDir()
	manifestRel, root := createSampleManifest(t)
	// move manifest under subdir to force interactive selection
	subDir := filepath.Join(temp, "proj")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	if err := os.Rename(manifestRel, filepath.Join(subDir, "dockform.yml")); err != nil {
		t.Fatalf("move manifest: %v", err)
	}
	manifestPath := filepath.Join(subDir, "dockform.yml")
	if err := os.Chdir(temp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(root) })

	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "")
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)
	cmd.SetContext(context.Background())

	pr := ui.StdPrinter{Out: slave, Err: slave}
	type result struct {
		cfg *manifest.Config
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		cfg, err := LoadConfigWithWarnings(cmd, pr)
		resCh <- result{cfg: cfg, err: err}
	}()
	time.Sleep(50 * time.Millisecond)
	_, _ = master.Write([]byte{'\r'})
	res := <-resCh
	if res.err != nil {
		t.Fatalf("LoadConfigWithWarnings interactive: %v", res.err)
	}
	if res.cfg == nil {
		t.Fatalf("expected config from interactive selection")
	}
	if got, _ := cmd.Flags().GetString("config"); true {
		gotEval, _ := filepath.EvalSymlinks(got)
		wantEval, _ := filepath.EvalSymlinks(manifestPath)
		if filepath.Clean(gotEval) != filepath.Clean(wantEval) {
			t.Fatalf("expected config flag to be updated, got %q want %q", gotEval, wantEval)
		}
	}
}

func TestSelectManifestPathNonTTYReturnsFalse(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewReader(nil))
	cmd.SetOut(io.Discard)
	cmd.Flags().String("config", "", "")
	okPath, ok, err := SelectManifestPath(cmd, ui.StdPrinter{}, t.TempDir(), 1, "")
	if err != nil {
		t.Fatalf("SelectManifestPath non-tty: %v", err)
	}
	if ok || okPath != "" {
		t.Fatalf("expected non-tty selection to return false")
	}
}

func TestSelectManifestPathTTY(t *testing.T) {
	temp := t.TempDir()
	stackDir := filepath.Join(temp, "stack")
	if err := os.MkdirAll(stackDir, 0o755); err != nil {
		t.Fatalf("mkdir stack: %v", err)
	}
	dc := filepath.Join(stackDir, "docker-compose.yml")
	if err := os.WriteFile(dc, []byte("version: '3'\nservices: {}\n"), 0o644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	manifestPath := filepath.Join(temp, "dockform.yml")
	writeManifest(t, manifestPath, "docker:\n  context: default\n")

	master, slave := openTTYOrSkip(t)
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Fatalf("close master pty: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Fatalf("close slave pty: %v", err)
		}
	})
	drainTTY(t, master)

	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "")
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)

	resCh := make(chan struct {
		path string
		ok   bool
		err  error
	}, 1)
	go func() {
		path, ok, err := SelectManifestPath(cmd, ui.StdPrinter{Out: slave, Err: slave}, temp, 1, "")
		resCh <- struct {
			path string
			ok   bool
			err  error
		}{path, ok, err}
	}()
	time.Sleep(50 * time.Millisecond)
	_, _ = master.Write([]byte{'\r'})
	res := <-resCh
	if res.err != nil {
		t.Fatalf("SelectManifestPath tty: %v", res.err)
	}
	if !res.ok || filepath.Clean(res.path) != filepath.Clean(manifestPath) {
		t.Fatalf("expected manifest path %q got ok=%v path=%q", manifestPath, res.ok, res.path)
	}
}

func TestSelectManifestPathTTYNoFiles(t *testing.T) {
	master, slave := openTTYOrSkip(t)
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Fatalf("close master pty: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Fatalf("close slave pty: %v", err)
		}
	})
	drainTTY(t, master)

	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "")
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)

	type result struct {
		path string
		ok   bool
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		path, ok, err := SelectManifestPath(cmd, ui.StdPrinter{Out: slave, Err: slave}, t.TempDir(), 1, "")
		resCh <- result{path: path, ok: ok, err: err}
	}()
	time.Sleep(50 * time.Millisecond)
	_, _ = master.Write([]byte{'\r'})
	res := <-resCh
	if res.err != nil {
		t.Fatalf("SelectManifestPath no files: %v", res.err)
	}
	if res.ok || res.path != "" {
		t.Fatalf("expected no selection when no manifests present")
	}
}

func TestSelectManifestPathTTYCancel(t *testing.T) {
	temp := t.TempDir()
	manifest := filepath.Join(temp, "dockform.yml")
	writeManifest(t, manifest, "docker:\n  context: default\n")

	master, slave := openTTYOrSkip(t)
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Fatalf("close master pty: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Fatalf("close slave pty: %v", err)
		}
	})
	drainTTY(t, master)

	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "")
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)

	resCh := make(chan struct {
		path string
		ok   bool
		err  error
	}, 1)
	go func() {
		path, ok, err := SelectManifestPath(cmd, ui.StdPrinter{Out: slave, Err: slave}, temp, 1, "")
		resCh <- struct {
			path string
			ok   bool
			err  error
		}{path, ok, err}
	}()
	time.Sleep(50 * time.Millisecond)
	_, _ = master.Write([]byte{''})
	res := <-resCh
	if res.err != nil {
		t.Fatalf("SelectManifestPath cancel: %v", res.err)
	}
	if res.ok || res.path != "" {
		t.Fatalf("expected cancel to return false selection")
	}
}

func TestSelectManifestPathTTYError(t *testing.T) {
	master, slave := openTTYOrSkip(t)
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Fatalf("close master pty: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Fatalf("close slave pty: %v", err)
		}
	})
	drainTTY(t, master)

	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "")
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)

	errorCh := make(chan error, 1)
	go func() {
		_, _, err := SelectManifestPath(cmd, ui.StdPrinter{Out: slave, Err: slave}, filepath.Join(t.TempDir(), "missing"), 1, "")
		errorCh <- err
	}()
	err := <-errorCh
	if err == nil {
		t.Fatalf("expected error when root directory missing")
	}
}

func TestCreateDockerClientAndValidate(t *testing.T) {
	var stub string
	if runtime.GOOS == "windows" {
		stub = `@echo off
if "%1"=="version" (
  echo 24.0.0
  exit /b 0
)
exit /b 0
`
	} else {
		stub = `#!/bin/sh
cmd="$1"; shift
if [ "$cmd" = "version" ]; then
  echo "24.0.0"
  exit 0
fi
exit 0
`
	}
	defer clitest.WithCustomDockerStub(t, stub)()

	cfg := &manifest.Config{
		Identifier: "demo",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
	}
	factory := CreateClientFactory()
	if factory == nil {
		t.Fatalf("expected client factory")
	}
	if err := ValidateWithFactory(context.Background(), cfg, factory); err != nil {
		t.Fatalf("ValidateWithFactory: %v", err)
	}
}

func TestCreatePlannerWithFactory(t *testing.T) {
	factory := CreateClientFactory()
	p := CreatePlannerWithFactory(factory, ui.StdPrinter{})
	if p == nil {
		t.Fatalf("expected planner")
	}
}

func TestSpinnerAndProgressOperations(t *testing.T) {
	master, slave := openTTYOrSkip(t)
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Fatalf("close master pty: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Fatalf("close slave pty: %v", err)
		}
	})
	drainTTY(t, master)

	std := ui.StdPrinter{Out: slave, Err: slave}

	called := false
	if err := SpinnerOperation(std, "Working...", func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("SpinnerOperation: %v", err)
	}
	if !called {
		t.Fatalf("expected spinner operation to run")
	}

	dynamicSpinnerCalled := false
	if err := DynamicSpinnerOperation(std, "Applying", func(s *ui.Spinner) error {
		if s == nil {
			t.Fatalf("expected spinner")
		}
		dynamicSpinnerCalled = true
		return nil
	}); err != nil {
		t.Fatalf("DynamicSpinnerOperation: %v", err)
	}
	if !dynamicSpinnerCalled {
		t.Fatalf("expected dynamic spinner operation to run")
	}
}

func TestRunWithRollingOrDirectPaths(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	out, used, err := RunWithRollingOrDirect(cmd, true, func(ctx context.Context) (string, error) {
		return "direct", nil
	})
	if err != nil || used || out != "direct" {
		t.Fatalf("expected direct execution, got out=%q used=%v err=%v", out, used, err)
	}

	master, slave := openTTYOrSkip(t)
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Fatalf("close master pty: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Fatalf("close slave pty: %v", err)
		}
	})
	drainTTY(t, master)

	cmdTTY := &cobra.Command{}
	cmdTTY.SetContext(context.Background())
	cmdTTY.SetIn(slave)
	cmdTTY.SetOut(slave)
	cmdTTY.SetErr(slave)

	out, used, err = RunWithRollingOrDirect(cmdTTY, false, func(ctx context.Context) (string, error) {
		return "tui", nil
	})
	if err != nil {
		t.Fatalf("RunWithRollingOrDirect tty: %v", err)
	}
	if !used || out != "tui" {
		t.Fatalf("expected rolling log to be used, got used=%v out=%q", used, out)
	}
}

func TestCLIContextOperations(t *testing.T) {
	cfg := &manifest.Config{
		Identifier: "demo",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
		Stacks: map[string]manifest.Stack{
			"default/app": {Root: ".", Files: nil},
		},
	}
	std := ui.StdPrinter{Out: io.Discard, Err: io.Discard}
	ctx := &CLIContext{
		Ctx:     context.Background(),
		Config:  cfg,
		Printer: std,
		Planner: planner.New(),
	}
	if _, err := ctx.BuildPlan(); err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}
	if err := ctx.ApplyPlan(); err == nil {
		t.Fatalf("expected apply to fail without docker client")
	}
	if err := ctx.PrunePlan(); err == nil {
		t.Fatalf("expected prune to fail without docker client")
	}
	if _, err := ctx.BuildDestroyPlan(); err == nil {
		t.Fatalf("expected destroy plan to fail without docker client")
	}
	if err := ctx.ExecuteDestroy(context.Background()); err == nil {
		t.Fatalf("expected destroy execution to fail without docker client")
	}
}

func TestGetConfirmationFlows(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("yes\n"))
	cmd.SetOut(io.Discard)
	pr := ui.StdPrinter{Out: io.Discard, Err: io.Discard}

	ok, err := GetConfirmation(cmd, pr, ConfirmationOptions{})
	if err != nil || !ok {
		t.Fatalf("expected confirmation in non-tty mode")
	}

	ok, err = GetConfirmation(cmd, pr, ConfirmationOptions{SkipConfirmation: true})
	if err != nil || !ok {
		t.Fatalf("expected skip confirmation to succeed")
	}

	master, slave := openTTYOrSkip(t)
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Fatalf("close master pty: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Fatalf("close slave pty: %v", err)
		}
	})
	drainTTY(t, master)
	cmdTTY := &cobra.Command{}
	cmdTTY.SetIn(slave)
	cmdTTY.SetOut(slave)
	cmdTTY.SetErr(slave)
	resCh := make(chan struct {
		ok  bool
		err error
	}, 1)
	go func() {
		ok, err := GetConfirmation(cmdTTY, ui.StdPrinter{Out: slave, Err: slave}, ConfirmationOptions{})
		resCh <- struct {
			ok  bool
			err error
		}{ok, err}
	}()
	time.Sleep(50 * time.Millisecond)
	_, _ = master.Write([]byte("yes\r"))
	res := <-resCh
	if res.err != nil || !res.ok {
		t.Fatalf("expected tty confirmation to succeed, got ok=%v err=%v", res.ok, res.err)
	}

	resCh = make(chan struct {
		ok  bool
		err error
	}, 1)
	go func() {
		ok, err := GetConfirmation(cmdTTY, ui.StdPrinter{Out: slave, Err: slave}, ConfirmationOptions{})
		resCh <- struct {
			ok  bool
			err error
		}{ok, err}
	}()
	time.Sleep(50 * time.Millisecond)
	_, _ = master.Write([]byte("no\r"))
	res = <-resCh
	if res.err != nil || res.ok {
		t.Fatalf("expected tty cancellation when user types no")
	}
}

func TestGetDestroyConfirmationFlows(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("demo\n"))
	cmd.SetOut(io.Discard)
	pr := ui.StdPrinter{Out: io.Discard, Err: io.Discard}

	ok, err := GetDestroyConfirmation(cmd, pr, DestroyConfirmationOptions{Identifier: "demo"})
	if err != nil || !ok {
		t.Fatalf("expected destroy confirmation to succeed")
	}

	cmd = &cobra.Command{}
	cmd.SetIn(strings.NewReader("wrong\n"))
	cmd.SetOut(io.Discard)
	if ok, err := GetDestroyConfirmation(cmd, pr, DestroyConfirmationOptions{Identifier: "demo"}); err != nil || ok {
		t.Fatalf("expected destroy confirmation to fail for mismatched identifier")
	}

	master, slave := openTTYOrSkip(t)
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Fatalf("close master pty: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Fatalf("close slave pty: %v", err)
		}
	})
	drainTTY(t, master)
	cmdTTY := &cobra.Command{}
	cmdTTY.SetIn(slave)
	cmdTTY.SetOut(slave)
	cmdTTY.SetErr(slave)
	resCh := make(chan struct {
		ok  bool
		err error
	}, 1)
	go func() {
		ok, err := GetDestroyConfirmation(cmdTTY, ui.StdPrinter{Out: slave, Err: slave}, DestroyConfirmationOptions{Identifier: "demo"})
		resCh <- struct {
			ok  bool
			err error
		}{ok, err}
	}()
	time.Sleep(50 * time.Millisecond)
	_, _ = master.Write([]byte("demo\r"))
	res := <-resCh
	if res.err != nil || !res.ok {
		t.Fatalf("expected tty destroy confirmation to succeed, got ok=%v err=%v", res.ok, res.err)
	}

	resCh = make(chan struct {
		ok  bool
		err error
	}, 1)
	go func() {
		ok, err := GetDestroyConfirmation(cmdTTY, ui.StdPrinter{Out: slave, Err: slave}, DestroyConfirmationOptions{Identifier: "demo"})
		resCh <- struct {
			ok  bool
			err error
		}{ok, err}
	}()
	time.Sleep(50 * time.Millisecond)
	_, _ = master.Write([]byte("nope\r"))
	res = <-resCh
	if res.err != nil || res.ok {
		t.Fatalf("expected destroy confirmation to cancel when identifier mismatches")
	}
}

func TestSetupCLIContextSuccess(t *testing.T) {
	var stub string
	if runtime.GOOS == "windows" {
		stub = `@echo off
if "%1"=="version" (
  echo 24.0.0
  exit /b 0
)
exit /b 0
`
	} else {
		stub = `#!/bin/sh
cmd="$1"; shift
if [ "$cmd" = "version" ]; then
  echo "24.0.0"
  exit 0
fi
exit 0
`
	}
	defer clitest.WithCustomDockerStub(t, stub)()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "dockform.yml")
	writeManifest(t, manifestPath, "identifier: demo\ncontexts:\n  default: {}\n")

	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "")
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("config", manifestPath); err != nil {
		t.Fatalf("set config flag: %v", err)
	}

	ctx, err := SetupCLIContext(cmd)
	if err != nil {
		t.Fatalf("SetupCLIContext: %v", err)
	}
	if ctx == nil || ctx.Config == nil || ctx.Factory == nil || ctx.Planner == nil {
		t.Fatalf("expected cli context to be fully populated")
	}
}

func TestSetupCLIContextError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if _, err := SetupCLIContext(cmd); err == nil {
		t.Fatalf("expected setup to fail when config missing")
	}
}

func TestMaskSecretsSimpleStrategiesAdditional(t *testing.T) {
	stack := manifest.Stack{}
	yaml := "password: hunter2\nquoted: \"secret\"\napi_key: token=abcd"
	if masked := MaskSecretsSimple(yaml, stack, "full"); strings.Contains(masked, "hunter2") {
		t.Fatalf("expected full mask to remove secrets: %s", masked)
	}
	partial := MaskSecretsSimple(yaml, stack, "partial")
	if !strings.Contains(partial, "hu***r2") {
		t.Fatalf("expected partial mask to preserve edges: %s", partial)
	}
	preserve := MaskSecretsSimple(yaml, stack, "preserve-length")
	if !strings.Contains(preserve, "******") {
		t.Fatalf("expected preserve-length mask, got %s", preserve)
	}
	if strings.Contains(preserve, "abcd") {
		t.Fatalf("expected token value to be redacted, got %s", preserve)
	}
}
