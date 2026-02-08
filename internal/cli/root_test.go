package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/cli/clitest"
)

func TestRoot_HasSubcommandsAndManifestFlag(t *testing.T) {
	cmd := newRootCmd()
	if cmd.PersistentFlags().Lookup("manifest") == nil {
		t.Fatalf("expected persistent --manifest flag on root command")
	}
	foundPlan := false
	foundApply := false
	foundValidate := false
	foundSecret := false
	foundManifest := false
	for _, c := range cmd.Commands() {
		if c.Name() == "plan" {
			foundPlan = true
		}
		if c.Name() == "apply" {
			foundApply = true
		}
		if c.Name() == "validate" {
			foundValidate = true
		}
		if c.Name() == "secrets" {
			foundSecret = true
		}
		if c.Name() == "manifest" {
			foundManifest = true
		}
	}
	if !foundPlan || !foundApply || !foundValidate || !foundSecret || !foundManifest {
		t.Fatalf("expected plan, apply, validate, secrets, manifest subcommands; got plan=%v apply=%v validate=%v secrets=%v manifest=%v", foundPlan, foundApply, foundValidate, foundSecret, foundManifest)
	}
}

func TestRoot_ContextIsNotPersistentFlag(t *testing.T) {
	cmd := newRootCmd()
	if cmd.PersistentFlags().Lookup("context") != nil {
		t.Fatalf("did not expect persistent --context flag on root command")
	}
}

func TestRoot_ConfigFlagRemoved(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--config", "dockform.yml"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected unknown flag error for --config")
	}
	if !strings.Contains(err.Error(), "unknown flag: --config") {
		t.Fatalf("expected unknown --config flag error, got: %v", err)
	}
}

func TestRoot_ManifestShorthandRemoved(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "-c", "dockform.yml"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected unknown flag error for -c")
	}
	if !strings.Contains(err.Error(), "unknown shorthand flag: 'c' in -c") {
		t.Fatalf("expected unknown shorthand -c flag error, got: %v", err)
	}
}

func TestRoot_VersionFlagPrints(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --version: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, Version()) {
		t.Fatalf("version output should contain version; got: %q", got)
	}
}

func TestRoot_HelpShowsProjectHome(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --help: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Project home: https://github.com/gcstr/dockform") {
		t.Fatalf("help output missing project home; got: %q", got)
	}
}

func TestRoot_SilenceFlags(t *testing.T) {
	cmd := newRootCmd()
	if !cmd.SilenceUsage {
		t.Fatalf("expected SilenceUsage to be true")
	}
	if !cmd.SilenceErrors {
		t.Fatalf("expected SilenceErrors to be true")
	}
}

func withFailingDockerRoot(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	stub := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "boom" 1>&2
    exit 1
    ;;
	esac
exit 1
`
	path := filepath.Join(dir, "docker")
	if runtime.GOOS == "windows" {
		path += ".cmd"
	}
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	old := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+old); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return func() { _ = os.Setenv("PATH", old) }
}

func TestExecute_ReturnCodes_ByErrorKind(t *testing.T) {
	// InvalidInput via bad YAML
	badFile, err := os.CreateTemp("", "dockform-bad-*.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(badFile.Name()) }()
	_, _ = badFile.WriteString("docker: foo\n")
	_ = badFile.Close()
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"dockform", "validate", "--manifest", badFile.Name()}
	if code := Execute(context.Background()); code != 2 {
		t.Fatalf("expected exit code 2 for invalid input, got %d", code)
	}

	// NotFound default mapping -> 1
	os.Args = []string{"dockform", "validate", "--manifest", "/path/does/not/exist.yml"}
	if code := Execute(context.Background()); code != 1 {
		t.Fatalf("expected exit code 1 for not found, got %d", code)
	}

	// Unavailable -> 69 (stub failing docker)
	defer withFailingDockerRoot(t)()
	cfg := clitest.BasicConfigPath(t)
	os.Args = []string{"dockform", "validate", "--manifest", cfg}
	if code := Execute(context.Background()); code != 69 {
		t.Fatalf("expected exit code 69 for unavailable, got %d", code)
	}
}

func TestPrintUserFriendly_VerboseAndHints(t *testing.T) {
	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() { os.Stderr = old }()

	verbose = true
	err := apperr.Wrap("unit", apperr.Unavailable, errors.New("daemon down"), "cannot reach docker")
	printUserFriendly(err)
	_ = w.Close()
	b, _ := io.ReadAll(r)
	s := string(b)
	if !strings.Contains(s, "Error: cannot reach docker") {
		t.Fatalf("missing short error: %s", s)
	}
	if !strings.Contains(s, "Detail:") {
		t.Fatalf("missing detail section: %s", s)
	}
	if !strings.Contains(s, "Is the Docker daemon running") {
		t.Fatalf("missing hint: %s", s)
	}
}

func TestPrintUserFriendly_NonAppErr(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() { os.Stderr = old }()
	verbose = false
	printUserFriendly(errors.New("plain"))
	_ = w.Close()
	b, _ := io.ReadAll(r)
	if !strings.Contains(string(b), "Error: plain") {
		t.Fatalf("expected plain error output, got: %s", string(b))
	}
}

func TestExecuteContextCanceled(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"dockform", "--help"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := Execute(ctx); code != 130 {
		t.Fatalf("expected exit code 130 for canceled context, got %d", code)
	}
}

func TestProvideExternalErrorHints(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() { os.Stderr = old }()

	provideExternalErrorHints(errors.New("invalid compose file at line 1"))
	_ = w.Close()
	out, _ := io.ReadAll(r)
	s := string(out)
	if !strings.Contains(s, "Hint: Check your Docker Compose file syntax") {
		t.Fatalf("expected compose syntax hint, got: %s", s)
	}
}

func TestProvideDockerTroubleshootingHintsNonDefaultContext(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() { os.Stderr = old }()

	provideDockerTroubleshootingHints(errors.New("context=my-prod docker daemon not reachable"))
	_ = w.Close()
	out, _ := io.ReadAll(r)
	s := string(out)
	if !strings.Contains(s, "docker context ls") {
		t.Fatalf("expected context troubleshooting hint, got: %s", s)
	}
	if !strings.Contains(s, "docker --context <name> ps") {
		t.Fatalf("expected context ps hint, got: %s", s)
	}
}
