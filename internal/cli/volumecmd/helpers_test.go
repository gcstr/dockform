package volumecmd

import (
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

func TestComputeSpecHashDeterministic(t *testing.T) {
	detailsA := dockercli.VolumeDetails{
		Driver:  "local",
		Options: map[string]string{"o": "uid=1000", "type": "nfs"},
		Labels:  map[string]string{"a": "1", "b": "2"},
	}
	detailsB := dockercli.VolumeDetails{
		Driver:  "local",
		Options: map[string]string{"type": "nfs", "o": "uid=1000"},
		Labels:  map[string]string{"b": "2", "a": "1"},
	}
	if computeSpecHash(detailsA) != computeSpecHash(detailsB) {
		t.Fatalf("expected spec hash to be order independent")
	}
}

func TestManifestHasVolume(t *testing.T) {
	cfg := &manifest.Config{
		Volumes: map[string]manifest.TopLevelResourceSpec{"data": {}},
		Filesets: map[string]manifest.FilesetSpec{
			"web": {TargetVolume: "cache"},
		},
	}
	tests := []struct {
		name string
		vol  string
		want bool
	}{
		{"direct", "data", true},
		{"fileset", "cache", true},
		{"missing", "tmp", false},
	}
	for _, tc := range tests {
		if got := manifestHasVolume(cfg, tc.vol); got != tc.want {
			t.Fatalf("%s: manifestHasVolume(%q)=%v, want %v", tc.name, tc.vol, got, tc.want)
		}
	}
}
