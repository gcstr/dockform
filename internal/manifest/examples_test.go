package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExamplesLoad loads every manifest shipped under examples/, so a schema
// change that breaks one fails here instead of in front of a new user.
func TestExamplesLoad(t *testing.T) {
	manifests, err := filepath.Glob(filepath.Join("..", "..", "examples", "*", "dockform.y*ml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) == 0 {
		t.Fatal("found no example manifests; the glob is wrong or the examples moved")
	}
	for _, path := range manifests {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			cfg, missing, err := LoadWithWarnings(path)
			if err != nil {
				t.Fatalf("load %s: %v", path, err)
			}
			if len(missing) > 0 {
				t.Errorf("example needs unset environment variables: %v", missing)
			}
			for name, fs := range cfg.DiscoveredFilesets {
				info, err := os.Stat(fs.SourceAbs)
				if err != nil {
					t.Errorf("fileset %s: source %s: %v", name, fs.SourceAbs, err)
					continue
				}
				if !info.IsDir() {
					t.Errorf("fileset %s: source %s is not a directory", name, fs.SourceAbs)
				}
			}
		})
	}
}
