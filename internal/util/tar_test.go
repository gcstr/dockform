package util

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestTarDirectoryToWriter_BasicAndPrefix(t *testing.T) {
	dir := t.TempDir()
	// Create structure: dir/a.txt, dir/sub/b.txt
	mustWriteFile(t, filepath.Join(dir, "a.txt"), []byte("A"))
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWriteFile(t, filepath.Join(sub, "b.txt"), []byte("BB"))

	var buf bytes.Buffer
	if err := TarDirectoryToWriter(dir, "prefix", &buf); err != nil {
		t.Fatalf("tar: %v", err)
	}
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	var names []string
	contents := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		names = append(names, hdr.Name)
		if hdr.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(tr)
			contents[hdr.Name] = string(b)
		}
	}
	sort.Strings(names)
	// Expect directory headers to end with '/'
	expect := []string{"prefix/a.txt", "prefix/sub/", "prefix/sub/b.txt"}
	if !equalSlices(names, expect) {
		t.Fatalf("unexpected tar entries:\n got: %#v\nwant: %#v", names, expect)
	}
	if contents["prefix/a.txt"] != "A" || contents["prefix/sub/b.txt"] != "BB" {
		t.Fatalf("unexpected file contents: %#v", contents)
	}
}

func TestTarFilesToWriter_SkipsSymlinks(t *testing.T) {
	// Skip on Windows where symlinks are not reliably supported in tests
	// The tar behavior must ignore symlinks regardless.
	if isWindows() {
		t.Skip("symlinks not supported on windows in tests")
	}
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), []byte("A"))
	if err := os.Symlink("a.txt", filepath.Join(dir, "link-to-a")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	var buf bytes.Buffer
	if err := TarFilesToWriter(dir, []string{"a.txt", "link-to-a"}, &buf); err != nil {
		t.Fatalf("tar files: %v", err)
	}
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		names = append(names, hdr.Name)
	}
	sort.Strings(names)
	expect := []string{"a.txt"}
	if !equalSlices(names, expect) {
		t.Fatalf("expected only regular files, got: %#v", names)
	}
}

func isWindows() bool {
	// Avoid importing runtime in multiple places; thin wrapper
	return strings.Contains(strings.ToLower(os.Getenv("OS")), "windows")
}

func mustWriteFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}
