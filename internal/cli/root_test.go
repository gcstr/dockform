package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
)

func TestRoot_HasSubcommandsAndConfigFlag(t *testing.T) {
	cmd := newRootCmd()
	if cmd.PersistentFlags().Lookup("config") == nil {
		t.Fatalf("expected persistent --config flag on root command")
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
		if c.Name() == "secret" {
			foundSecret = true
		}
		if c.Name() == "manifest" {
			foundManifest = true
		}
	}
	if !foundPlan || !foundApply || !foundValidate || !foundSecret || !foundManifest {
		t.Fatalf("expected plan, apply, validate, secret, manifest subcommands; got plan=%v apply=%v validate=%v secret=%v manifest=%v", foundPlan, foundApply, foundValidate, foundSecret, foundManifest)
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
	if !strings.Contains(got, Version()+"\n") {
		t.Fatalf("version output mismatch; got: %q", got)
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

func TestPlan_HasPruneFlag(t *testing.T) {
	cmd := newPlanCmd()
	if cmd.Flags().Lookup("prune") == nil {
		t.Fatalf("expected --prune flag on plan command")
	}
}

func TestApply_HasPruneFlag(t *testing.T) {
	cmd := newApplyCmd()
	if cmd.Flags().Lookup("prune") == nil {
		t.Fatalf("expected --prune flag on apply command")
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
	os.Args = []string{"dockform", "validate", "-c", badFile.Name()}
	if code := Execute(); code != 2 {
		t.Fatalf("expected exit code 2 for invalid input, got %d", code)
	}

	// NotFound default mapping -> 1
	os.Args = []string{"dockform", "validate", "-c", "/path/does/not/exist.yml"}
	if code := Execute(); code != 1 {
		t.Fatalf("expected exit code 1 for not found, got %d", code)
	}

	// Unavailable -> 69 (stub failing docker)
	defer withFailingDockerRoot(t)()
	cfg := basicConfigPath(t)
	os.Args = []string{"dockform", "validate", "-c", cfg}
	if code := Execute(); code != 69 {
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
