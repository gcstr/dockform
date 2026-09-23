package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadManifest(t *testing.T, yml string) (Config, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dockform.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(dir)
}

func TestDestroyFlag_ParsesOnVolumesAndNetworks(t *testing.T) {
	cfg, err := loadManifest(t, `identifier: demo
contexts:
  default:
    volumes:
      data:
        destroy: false
      scratch: {}
      explicit:
        destroy: true
    networks:
      keepnet:
        destroy: false
      plainnet: {}
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	vols := cfg.Contexts["default"].Volumes
	nets := cfg.Contexts["default"].Networks
	checks := []struct {
		what string
		got  bool
		want bool
	}{
		{"volume data (destroy: false)", vols["data"].Kept(), true},
		{"volume scratch (omitted)", vols["scratch"].Kept(), false},
		{"volume explicit (destroy: true)", vols["explicit"].Kept(), false},
		{"network keepnet (destroy: false)", nets["keepnet"].Kept(), true},
		{"network plainnet (omitted)", nets["plainnet"].Kept(), false},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: Kept() = %v, want %v", c.what, c.got, c.want)
		}
	}
}

// A typo must not silently turn into "destroy it": the strict loader rejects it.
func TestDestroyFlag_MisspelledKeyIsRejected(t *testing.T) {
	_, err := loadManifest(t, `identifier: demo
contexts:
  default:
    volumes:
      data:
        destory: false
`)
	if err == nil {
		t.Fatal("expected a misspelled destroy key to be rejected")
	}
	if !strings.Contains(err.Error(), "destory") {
		t.Errorf("error should name the unknown key, got: %v", err)
	}
}
