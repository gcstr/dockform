package util

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

	// Add a symlink
	if runtime.GOOS != "windows" {
		if err := os.Symlink("a.txt", filepath.Join(dir, "link-to-a")); err != nil {
			t.Fatalf("symlink: %v", err)
		}
	}

	var buf bytes.Buffer
	if err := TarDirectoryToWriter(dir, "prefix", &buf); err != nil {
		t.Fatalf("tar: %v", err)
	}
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	var names []string
	types := map[string]byte{}
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
		types[hdr.Name] = hdr.Typeflag
		if hdr.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(tr)
			contents[hdr.Name] = string(b)
		}
	}
	sort.Strings(names)
	// Expect directory headers to end with '/'
	expect := []string{"prefix/a.txt", "prefix/link-to-a", "prefix/sub/", "prefix/sub/b.txt"}
	// On Windows, symlink may not be created
	if runtime.GOOS == "windows" {
		expect = []string{"prefix/a.txt", "prefix/sub/", "prefix/sub/b.txt"}
	}
	if !equalSlices(names, expect) {
		t.Fatalf("unexpected tar entries:\n got: %#v\nwant: %#v", names, expect)
	}
	if contents["prefix/a.txt"] != "A" || contents["prefix/sub/b.txt"] != "BB" {
		t.Fatalf("unexpected file contents: %#v", contents)
	}
	if runtime.GOOS != "windows" && types["prefix/link-to-a"] != tar.TypeSymlink {
		t.Fatalf("expected link-to-a to be symlink, got type %d", types["prefix/link-to-a"])
	}
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
