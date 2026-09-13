package runlog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// entry is the line written into .gitignore.
const entry = ".dockform/"

// EnsureGitignored appends the run-log directory to dir/.gitignore unless it is
// already listed. Reports whether it wrote anything.
func EnsureGitignored(dir string) (bool, error) {
	path := filepath.Join(dir, ".gitignore")

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, line := range strings.Split(string(existing), "\n") {
		// Git strips only TRAILING whitespace from a gitignore line, not
		// leading: a hand-indented "  .dockform/" does not ignore anything to
		// git, so TrimSpace here would wrongly treat it as a match, decline to
		// append a working entry, and leave the directory genuinely unignored.
		if strings.TrimRight(line, " \t\r") == entry {
			return false, nil
		}
	}

	var b strings.Builder
	b.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteByte('\n')
	}
	b.WriteString(entry + "\n")

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// IsIgnored asks git whether the run-log directory is ignored. Callers use the
// result to decide whether to warn, so it reports true (nothing to warn about)
// both when the path is genuinely ignored and when dir is not inside a git
// work tree at all (or git is unavailable) — there is no git history to commit
// the log into either way, and warning on every apply against an un-versioned
// directory would make the warning noise, not signal.
func IsIgnored(dir string) (bool, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	if err := cmd.Run(); err != nil {
		return true, nil // not a work tree, or no git: stay quiet
	}

	// check-ignore exits 0 when the path IS ignored, 1 when it is not. Any other
	// non-nil error (a fatal git failure) is deliberately folded into "not
	// ignored" too: the only case this function reports true is a confirmed
	// exit 0, so the conflation can only ever add a spurious warning in a rare
	// git-failure edge case, never suppress a warranted one.
	check := exec.Command("git", "-C", dir, "check-ignore", "-q", entry)
	if err := check.Run(); err != nil {
		return false, nil
	}
	return true, nil
}
