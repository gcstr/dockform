package applycmd_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/clitest"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestApply_WarnsWhenDockformDirNotGitignored exercises the Task 8 wiring
// end-to-end: when the manifest dir is a git work tree that does not ignore
// .dockform/, apply must warn (the run log could otherwise be committed).
func TestApply_WarnsWhenDockformDirNotGitignored(t *testing.T) {
	requireGit(t)
	defer clitest.WithStubDocker(t)()

	cfgPath := clitest.BasicConfigPath(t)
	manifestDir := filepath.Dir(cfgPath)
	runGit(t, manifestDir, "init", "-q")

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("no\n"))
	root.SetArgs([]string{"apply", "--manifest", cfgPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("apply execute: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, ".dockform/ is not gitignored") {
		t.Fatalf("expected a gitignore warning; got: %s", got)
	}
}

// TestApply_NoWarningWhenDockformDirIsGitignored is the negative counterpart:
// once .dockform/ is listed in .gitignore, apply must stay quiet.
func TestApply_NoWarningWhenDockformDirIsGitignored(t *testing.T) {
	requireGit(t)
	defer clitest.WithStubDocker(t)()

	cfgPath := clitest.BasicConfigPath(t)
	manifestDir := filepath.Dir(cfgPath)
	runGit(t, manifestDir, "init", "-q")

	gitignorePath := filepath.Join(manifestDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(".dockform/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("no\n"))
	root.SetArgs([]string{"apply", "--manifest", cfgPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("apply execute: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "is not gitignored") {
		t.Fatalf("expected no gitignore warning; got: %s", got)
	}
}
