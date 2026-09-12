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
	// 14 files, lexicographically ordered by timestamp like real ones.
	for i := 0; i < 14; i++ {
		name := filepath.Join(logs, "apply-202609120000"+string(rune('a'+i))+".log")
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := prune(logs, 10); err != nil {
		t.Fatalf("prune: %v", err)
	}

	entries, err := os.ReadDir(logs)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 10 {
		t.Fatalf("kept %d logs, want 10", len(entries))
	}
	// The survivors must be the newest, i.e. the lexicographically largest.
	if entries[0].Name() != "apply-202609120000e.log" {
		t.Fatalf("oldest survivor = %q, want apply-202609120000e.log", entries[0].Name())
	}
}
