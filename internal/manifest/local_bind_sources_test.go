package manifest

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLocalBindSources(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "stack")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	otherRoot := filepath.Join(outside, "stk")

	tests := []struct {
		name    string
		sources []string
		dirs    []string
		want    []string
	}{
		{"relative mount resolved into the stack root", []string{filepath.Join(root, "cfg")}, []string{base, root}, []string{filepath.Join(root, "cfg")}},
		{"mount from an include file inside the tree", []string{filepath.Join(base, "inc", "fromincl")}, []string{base, root}, []string{filepath.Join(base, "inc", "fromincl")}},
		{"stack root outside the manifest dir", []string{filepath.Join(otherRoot, "data")}, []string{base, otherRoot}, []string{filepath.Join(otherRoot, "data")}},
		{"the project dir itself", []string{root}, []string{base, root}, []string{root}},
		{"docker socket is left alone", []string{"/var/run/docker.sock"}, []string{base, root}, nil},
		{"a path outside the project is left alone", []string{filepath.Join(outside, "secrets")}, []string{base, root}, nil},
		{"a sibling sharing the prefix is not inside", []string{base + "-other/x"}, []string{base}, nil},
		{"empty project dirs are ignored", []string{"/var/run/docker.sock"}, []string{"", ""}, nil},
		{"sorted and de-duplicated", []string{filepath.Join(root, "b"), filepath.Join(root, "a"), filepath.Join(root, "b")}, []string{root}, []string{filepath.Join(root, "a"), filepath.Join(root, "b")}},
		{"a project dir of / is ignored rather than matching everything", []string{"/var/run/docker.sock", "/srv/x"}, []string{"/"}, nil},
		{"a / project dir does not disable the other one", []string{filepath.Join(root, "cfg"), "/var/run/docker.sock"}, []string{"/", root}, []string{filepath.Join(root, "cfg")}},
		{"a trailing slash is the same source", []string{filepath.Join(root, "a"), filepath.Join(root, "a") + "/"}, []string{root}, []string{filepath.Join(root, "a")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LocalBindSources(tt.sources, tt.dirs...)
			if !slices.Equal(got, tt.want) {
				t.Errorf("LocalBindSources(%v, %v) = %v, want %v", tt.sources, tt.dirs, got, tt.want)
			}
		})
	}
}

// Compose keeps paths as given and does not resolve symlinks, while a project
// dir may be spelled either way; on macOS /var is a symlink to /private/var.
// A source that does not exist yet must still match through the link.
func TestLocalBindSources_MatchesThroughASymlink(t *testing.T) {
	realDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(realDir, "stack"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	viaLink := filepath.Join(link, "stack", "cfg")    // does not exist
	viaReal := filepath.Join(realDir, "stack", "cfg") // does not exist

	if got := LocalBindSources([]string{viaReal}, link); !slices.Equal(got, []string{viaReal}) {
		t.Errorf("source spelled through the real path, dir through the link: got %v", got)
	}
	if got := LocalBindSources([]string{viaLink}, realDir); !slices.Equal(got, []string{viaLink}) {
		t.Errorf("source spelled through the link, dir through the real path: got %v", got)
	}
}
