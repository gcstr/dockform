package assets

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestBuildLocalManifest_BasicAndExcludes(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	mustWrite(filepath.Join(dir, "a.txt"), "A")
	mustWrite(filepath.Join(dir, "sub", "b.txt"), "BB")
	mustWrite(filepath.Join(dir, "ignore.txt"), "X")
	mustWrite(filepath.Join(dir, "notes.bak"), "Y")
	mustWrite(filepath.Join(dir, "temp123.txt"), "Z")
	// symlink should be ignored
	_ = os.Symlink(filepath.Join(dir, "a.txt"), filepath.Join(dir, "sub", "link-to-a"))

	m, err := BuildLocalManifest(dir, "/target", []string{"ignore.txt", "*.bak", "temp*"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if m.Version != "v1" {
		t.Fatalf("version: %s", m.Version)
	}
	if m.Target != "/target" {
		t.Fatalf("target: %s", m.Target)
	}
	if m.TreeHash == "" {
		t.Fatalf("expected tree hash")
	}

	// Expect only a.txt and sub/b.txt
	paths := make([]string, 0, len(m.Files))
	for _, f := range m.Files {
		paths = append(paths, f.Path)
	}
	if runtime.GOOS == "windows" {
		// Sanity: ensure we used forward slashes in paths
		for _, p := range paths {
			if strings.Contains(p, "\\") {
				t.Fatalf("expected forward slashes in path: %s", p)
			}
		}
	}
	want := []string{"a.txt", "sub/b.txt"}
	if len(paths) != len(want) {
		t.Fatalf("paths len=%d want=%d (%v)", len(paths), len(want), paths)
	}
	for i, p := range paths {
		if p != want[i] {
			t.Fatalf("paths[%d]=%s want=%s (all=%v)", i, p, want[i], paths)
		}
	}
	// Ensure sizes are correct
	sizes := map[string]int64{}
	for _, f := range m.Files {
		sizes[f.Path] = f.Size
	}
	if sizes["a.txt"] != 1 || sizes["sub/b.txt"] != 2 {
		t.Fatalf("sizes: %+v", sizes)
	}
}

func TestParseManifestJSON_EmptyAndInvalid(t *testing.T) {
	m, err := ParseManifestJSON("")
	if err != nil {
		t.Fatalf("empty parse error: %v", err)
	}
	if m.Version != "v1" || m.Files != nil {
		t.Fatalf("unexpected default: %+v", m)
	}

	if _, err := ParseManifestJSON("{"); err == nil {
		t.Fatalf("expected error for invalid json")
	}
}

func TestManifest_ToJSON_RoundTrip(t *testing.T) {
	m := Manifest{
		Version:  "v1",
		Target:   "/t",
		Exclude:  []string{"*.tmp"},
		UID:      1000,
		GID:      1000,
		Files:    []FileEntry{{Path: "a.txt", Size: 1, Sha256: "aa"}, {Path: "b.txt", Size: 2, Sha256: "bb"}},
		TreeHash: "hash",
	}
	s, err := m.ToJSON()
	if err != nil {
		t.Fatalf("to json: %v", err)
	}
	m2, err := ParseManifestJSON(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m2.Target != m.Target || m2.UID != m.UID || m2.GID != m.GID || len(m2.Files) != len(m.Files) {
		t.Fatalf("round trip mismatch: %+v vs %+v", m2, m)
	}
}

func TestDiffManifests_CreateUpdateDelete_AndNoChange(t *testing.T) {
	// Build local from real files to ensure stable ordering
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("NEW"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	local, err := BuildLocalManifest(dir, "/t", nil)
	if err != nil {
		t.Fatalf("build local: %v", err)
	}

	// Case: no change -> empty diff
	d0 := DiffManifests(local, local)
	if len(d0.ToCreate) != 0 || len(d0.ToUpdate) != 0 || len(d0.ToDelete) != 0 {
		t.Fatalf("expected empty diff for equal manifests: %+v", d0)
	}

	// Remote: has a.txt with different size/hash, and has extra c.txt; missing b.txt
	remote := Manifest{Version: "v1", Target: "/t", Files: []FileEntry{{Path: "a.txt", Size: 1, Sha256: "old"}, {Path: "c.txt", Size: 1, Sha256: "ccc"}}}
	d := DiffManifests(local, remote)
	// ToCreate should include b.txt; ToUpdate include a.txt; ToDelete include c.txt
	has := func(paths []FileEntry, p string) bool {
		for _, f := range paths {
			if f.Path == p {
				return true
			}
		}
		return false
	}
	hasDel := func(paths []string, p string) bool {
		for _, f := range paths {
			if f == p {
				return true
			}
		}
		return false
	}
	if !has(d.ToCreate, "b.txt") {
		t.Fatalf("expected create b.txt: %+v", d)
	}
	if !has(d.ToUpdate, "a.txt") {
		t.Fatalf("expected update a.txt: %+v", d)
	}
	if !hasDel(d.ToDelete, "c.txt") {
		t.Fatalf("expected delete c.txt: %+v", d)
	}
	// Ensure sorted order within groups
	if !sort.SliceIsSorted(d.ToCreate, func(i, j int) bool { return d.ToCreate[i].Path < d.ToCreate[j].Path }) {
		t.Fatalf("create not sorted: %+v", d.ToCreate)
	}
	if !sort.SliceIsSorted(d.ToUpdate, func(i, j int) bool { return d.ToUpdate[i].Path < d.ToUpdate[j].Path }) {
		t.Fatalf("update not sorted: %+v", d.ToUpdate)
	}
	if !sort.StringsAreSorted(d.ToDelete) {
		t.Fatalf("delete not sorted: %+v", d.ToDelete)
	}
}
