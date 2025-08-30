package filesets

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestBuildLocalIndex_BasicAndExcludes(t *testing.T) {
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

	i, err := BuildLocalIndex(dir, "/target", []string{"ignore.txt", "*.bak", "temp*"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if i.Version != "v1" {
		t.Fatalf("version: %s", i.Version)
	}
	if i.Target != "/target" {
		t.Fatalf("target: %s", i.Target)
	}
	if i.TreeHash == "" {
		t.Fatalf("expected tree hash")
	}

	// Expect only a.txt and sub/b.txt
	paths := make([]string, 0, len(i.Files))
	for _, f := range i.Files {
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
	for i0, p := range paths {
		if p != want[i0] {
			t.Fatalf("paths[%d]=%s want=%s (all=%v)", i0, p, want[i0], paths)
		}
	}
	// Ensure sizes are correct
	sizes := map[string]int64{}
	for _, f := range i.Files {
		sizes[f.Path] = f.Size
	}
	if sizes["a.txt"] != 1 || sizes["sub/b.txt"] != 2 {
		t.Fatalf("sizes: %+v", sizes)
	}
}

func TestParseIndexJSON_EmptyAndInvalid(t *testing.T) {
	i, err := ParseIndexJSON("")
	if err != nil {
		t.Fatalf("empty parse error: %v", err)
	}
	if i.Version != "v1" || i.Files != nil {
		t.Fatalf("unexpected default: %+v", i)
	}

	if _, err := ParseIndexJSON("{"); err == nil {
		t.Fatalf("expected error for invalid json")
	}
}

func TestIndex_ToJSON_RoundTrip(t *testing.T) {
	i := Index{
		Version:  "v1",
		Target:   "/t",
		Exclude:  []string{"*.tmp"},
		UID:      1000,
		GID:      1000,
		Files:    []FileEntry{{Path: "a.txt", Size: 1, Sha256: "aa"}, {Path: "b.txt", Size: 2, Sha256: "bb"}},
		TreeHash: "hash",
	}
	s, err := i.ToJSON()
	if err != nil {
		t.Fatalf("to json: %v", err)
	}
	i2, err := ParseIndexJSON(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if i2.Target != i.Target || i2.UID != i.UID || i2.GID != i.GID || len(i2.Files) != len(i.Files) {
		t.Fatalf("round trip mismatch: %+v vs %+v", i2, i)
	}
}

func TestDiffIndexes_CreateUpdateDelete_AndNoChange(t *testing.T) {
	// Build local from real files to ensure stable ordering
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("NEW"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	local, err := BuildLocalIndex(dir, "/t", nil)
	if err != nil {
		t.Fatalf("build local: %v", err)
	}

	// Case: no change -> empty diff
	d0 := DiffIndexes(local, local)
	if len(d0.ToCreate) != 0 || len(d0.ToUpdate) != 0 || len(d0.ToDelete) != 0 {
		t.Fatalf("expected empty diff for equal indexes: %+v", d0)
	}

	// Remote: has a.txt with different size/hash, and has extra c.txt; missing b.txt
	remote := Index{Version: "v1", Target: "/t", Files: []FileEntry{{Path: "a.txt", Size: 1, Sha256: "old"}, {Path: "c.txt", Size: 1, Sha256: "ccc"}}}
	d := DiffIndexes(local, remote)
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
	if !sort.SliceIsSorted(d.ToCreate, func(i0, j int) bool { return d.ToCreate[i0].Path < d.ToCreate[j].Path }) {
		t.Fatalf("create not sorted: %+v", d.ToCreate)
	}
	if !sort.SliceIsSorted(d.ToUpdate, func(i0, j int) bool { return d.ToUpdate[i0].Path < d.ToUpdate[j].Path }) {
		t.Fatalf("update not sorted: %+v", d.ToUpdate)
	}
	if !sort.StringsAreSorted(d.ToDelete) {
		t.Fatalf("delete not sorted: %+v", d.ToDelete)
	}
}

func TestBuildLocalIndex_DirectoryPatternsAndGlobDoubleStar(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	mustWrite(filepath.Join(dir, "keep.txt"), "K")
	mustWrite(filepath.Join(dir, "tmp", "a.txt"), "A")
	mustWrite(filepath.Join(dir, "tmp", "sub", "b.txt"), "B")
	mustWrite(filepath.Join(dir, "secrets", "secret.txt"), "S")

	// Exclude tmp/** subtree and secrets/ directory via trailing slash semantics
	i, err := BuildLocalIndex(dir, "/assets", []string{"tmp/**", "secrets/"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got := make([]string, 0, len(i.Files))
	for _, f := range i.Files {
		got = append(got, f.Path)
	}
	sort.Strings(got)
	want := []string{"keep.txt"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("paths=%v want=%v", got, want)
	}
}

func TestBuildLocalIndex_DeterministicTwice(t *testing.T) {
	dir := t.TempDir()
	files := []struct{ p, s string }{
		{"a.txt", "1"},
		{"b/b.txt", "22"},
		{"c/c.tmp", "333"},
		{"c/d.tmp", "4444"},
	}
	for _, it := range files {
		p := filepath.Join(dir, it.p)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(it.s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	excludes := []string{"**/.DS_Store", "*.bak", "c/"}
	i1, err := BuildLocalIndex(dir, "/t", excludes)
	if err != nil {
		t.Fatalf("i1: %v", err)
	}
	i2, err := BuildLocalIndex(dir, "/t", excludes)
	if err != nil {
		t.Fatalf("i2: %v", err)
	}
	if i1.TreeHash != i2.TreeHash {
		t.Fatalf("tree hash mismatch: %s vs %s", i1.TreeHash, i2.TreeHash)
	}
	// Ensure files are sorted lexicographically
	if !sort.SliceIsSorted(i1.Files, func(i0, j int) bool { return i1.Files[i0].Path < i1.Files[j].Path }) {
		t.Fatalf("files not sorted: %+v", i1.Files)
	}
}
