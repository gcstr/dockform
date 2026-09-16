package runlog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitignoredCreatesEntry(t *testing.T) {
	dir := t.TempDir()

	added, err := EnsureGitignored(dir)
	if err != nil {
		t.Fatalf("EnsureGitignored: %v", err)
	}
	if !added {
		t.Fatal("expected the entry to be added")
	}

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(data), ".dockform/") {
		t.Fatalf(".gitignore missing the entry:\n%s", data)
	}
}

func TestEnsureGitignoredIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules\n.dockform/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	added, err := EnsureGitignored(dir)
	if err != nil {
		t.Fatalf("EnsureGitignored: %v", err)
	}
	if added {
		t.Fatal("entry already present; must not be added twice")
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if strings.Count(string(data), ".dockform/") != 1 {
		t.Fatalf("entry duplicated:\n%s", data)
	}
}

func TestEnsureGitignoredPreservesExistingContentAndNewline(t *testing.T) {
	dir := t.TempDir()
	// No trailing newline: appending must not join two entries on one line.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureGitignored(dir); err != nil {
		t.Fatalf("EnsureGitignored: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(data), "node_modules\n") {
		t.Fatalf("existing entry was corrupted:\n%q", data)
	}
	if strings.Contains(string(data), "node_modules.dockform") {
		t.Fatalf("entries were joined:\n%q", data)
	}
}

// TestIsIgnoredOutsideWorkTreeStaysQuiet pins the fix for a caller-facing bug:
// callers warn on !ignored, so IsIgnored must report true ("nothing to warn
// about") outside a git work tree, not false — otherwise every apply against
// an un-versioned manifest dir would spuriously warn.
func TestIsIgnoredOutsideWorkTreeStaysQuiet(t *testing.T) {
	dir := t.TempDir() // not a git repo

	ignored, err := IsIgnored(dir)
	if err != nil {
		t.Fatalf("IsIgnored: %v", err)
	}
	if !ignored {
		t.Fatal("expected IsIgnored to report true (no warning) outside a git work tree")
	}
}

func TestIsIgnoredInWorkTreeWithoutEntry(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")

	ignored, err := IsIgnored(dir)
	if err != nil {
		t.Fatalf("IsIgnored: %v", err)
	}
	if ignored {
		t.Fatal("expected IsIgnored to report false: .dockform/ is not listed")
	}
}

func TestIsIgnoredInWorkTreeWithEntry(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	if _, err := EnsureGitignored(dir); err != nil {
		t.Fatalf("EnsureGitignored: %v", err)
	}

	ignored, err := IsIgnored(dir)
	if err != nil {
		t.Fatalf("IsIgnored: %v", err)
	}
	if !ignored {
		t.Fatal("expected IsIgnored to report true: .dockform/ is listed in .gitignore")
	}
}

// TestEnsureGitignoredLeadingWhitespaceIsNotAMatch pins the fix for a
// caller-facing bug: git strips only TRAILING whitespace from a gitignore
// line, not leading, so a hand-indented "  .dockform/" does not ignore
// anything to git. Before the fix, strings.TrimSpace made EnsureGitignored
// think such a line already covered the entry, so it declined to append a
// working one and the directory was left genuinely unignored.
func TestEnsureGitignoredLeadingWhitespaceIsNotAMatch(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules\n  .dockform/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	added, err := EnsureGitignored(dir)
	if err != nil {
		t.Fatalf("EnsureGitignored: %v", err)
	}
	if !added {
		t.Fatal("expected a real entry to be appended: the existing line is indented and does not match git's own semantics")
	}

	ignored, err := IsIgnored(dir)
	if err != nil {
		t.Fatalf("IsIgnored: %v", err)
	}
	if !ignored {
		t.Fatal("expected the directory to actually be ignored after EnsureGitignored appended a working entry")
	}

	cmd := exec.Command("git", "-C", dir, "check-ignore", "-q", ".dockform/")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git check-ignore reports .dockform/ is NOT ignored despite the appended entry: %v", err)
	}
}

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
