package runlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreatesLogUnderManifestDir(t *testing.T) {
	dir := t.TempDir()

	h, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = h.Close() }()

	want := filepath.Join(dir, ".dockform", "logs")
	if !strings.HasPrefix(h.Path(), want) {
		t.Fatalf("Path() = %q, want a file under %q", h.Path(), want)
	}
	h.Logger().Info("hello_runlog")
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(h.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "hello_runlog") {
		t.Fatalf("log did not capture the record:\n%s", data)
	}
}

func TestOpenFallsBackWhenUnwritable(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skipf("cannot chmod: %v", err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()

	// A successful chmod does not guarantee the directory is actually
	// unwritable: running as root (e.g. inside a container) bypasses Unix
	// permission checks entirely, so a write below would silently succeed
	// and no fallback would occur. Probe real writability directly rather
	// than trusting the chmod's exit status.
	probe := filepath.Join(dir, ".runlog-write-probe")
	if f, err := os.Create(probe); err == nil {
		_ = f.Close()
		_ = os.Remove(probe)
		t.Skip("directory is writable despite chmod 0o500 (likely running as a user that bypasses permission checks, e.g. root); cannot exercise the fallback path")
	}

	h, err := Open(dir)
	if err != nil {
		t.Fatalf("Open must not fail when the manifest dir is unwritable: %v", err)
	}
	defer func() { _ = h.Close() }()

	if strings.HasPrefix(h.Path(), dir) {
		t.Fatalf("Path() = %q, expected a fallback outside the unwritable dir", h.Path())
	}
	if h.Warning() == "" {
		t.Fatal("a fallback must surface a warning")
	}
}

func TestPruneKeepsNewestTen(t *testing.T) {
	dir := t.TempDir()
	logs := filepath.Join(dir, ".dockform", "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	// keepRuns + 4 files, lexicographically ordered by timestamp like real ones.
	total := keepRuns + 4
	for i := 0; i < total; i++ {
		name := filepath.Join(logs, "apply-202609120000"+string(rune('a'+i))+".log")
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := prune(logs, keepRuns); err != nil {
		t.Fatalf("prune: %v", err)
	}

	entries, err := os.ReadDir(logs)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != keepRuns {
		t.Fatalf("kept %d logs, want %d", len(entries), keepRuns)
	}
	// The survivors must be the newest, i.e. the lexicographically largest.
	oldestSurvivor := "apply-202609120000" + string(rune('a'+total-keepRuns)) + ".log"
	if entries[0].Name() != oldestSurvivor {
		t.Fatalf("oldest survivor = %q, want %q", entries[0].Name(), oldestSurvivor)
	}
}

// TestPruneOnlyTouchesItsOwnRunLogs proves prune ignores files that do not
// match the apply-*.log shape Open generates: they must not be deleted, and
// they must not occupy a keep slot that a real run log would otherwise get.
func TestPruneOnlyTouchesItsOwnRunLogs(t *testing.T) {
	dir := t.TempDir()
	logs := filepath.Join(dir, ".dockform", "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}

	// keepRuns + 4 real run logs, lexicographically ordered like real ones.
	total := keepRuns + 4
	for i := 0; i < total; i++ {
		name := filepath.Join(logs, "apply-202609120000"+string(rune('a'+i))+".log")
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// A stray that sorts before "apply-" (would land in the delete range if
	// counted) and one that sorts after it (would occupy a keep slot).
	dsStore := filepath.Join(logs, ".DS_Store")
	notes := filepath.Join(logs, "zzz-notes.txt")
	if err := os.WriteFile(dsStore, []byte("mac metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notes, []byte("not a run log"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := prune(logs, keepRuns); err != nil {
		t.Fatalf("prune: %v", err)
	}

	if _, err := os.Stat(dsStore); err != nil {
		t.Fatalf(".DS_Store must survive prune, it is not a run log: %v", err)
	}
	if _, err := os.Stat(notes); err != nil {
		t.Fatalf("zzz-notes.txt must survive prune, it is not a run log: %v", err)
	}

	entries, err := os.ReadDir(logs)
	if err != nil {
		t.Fatal(err)
	}
	var runLogs int
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "apply-") && strings.HasSuffix(name, ".log") {
			runLogs++
		}
	}
	if runLogs != keepRuns {
		t.Fatalf("kept %d run logs, want %d (strays must not consume keep slots)", runLogs, keepRuns)
	}
	// Total entries = keepRuns run logs + the 2 strays that prune must leave alone.
	if len(entries) != keepRuns+2 {
		t.Fatalf("directory has %d entries, want %d (%d run logs + 2 strays)", len(entries), keepRuns+2, keepRuns)
	}
}
