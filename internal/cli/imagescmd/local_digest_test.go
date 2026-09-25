package imagescmd

import (
	"context"
	"errors"
	"testing"

	"github.com/gcstr/dockform/internal/images"
)

type fakeLocalImages struct {
	running   map[string]string // "project|service" → image ID
	digests   map[string]string // image ID → repo digest
	refs      []string          // docker image ls output
	refsErr   error
	inspected map[string]string // image ref → repo digest, for the no-container fallback
}

func (f *fakeLocalImages) ComposeContainerImageMap(context.Context) (map[string]string, error) {
	return f.running, nil
}

func (f *fakeLocalImages) ImageRepoDigestMap(context.Context, []string) (map[string]string, error) {
	return f.digests, nil
}

func (f *fakeLocalImages) LocalImageRefs(context.Context) ([]string, error) { return f.refs, f.refsErr }

func (f *fakeLocalImages) ImageInspectRepoDigests(_ context.Context, ref string) ([]string, error) {
	if d, ok := f.inspected[ref]; ok {
		return []string{"repo@" + d}, nil
	}
	return nil, errors.New("no such image")
}

func lookupWith(f *fakeLocalImages) images.LocalDigestFunc {
	return newLocalDigestFunc(func(string) localImageClient { return f }, map[string]string{"one/navidrome": "navidrome"})
}

// The reported bug: images upgrade rewrote the tag, nothing was applied yet.
// The host has 0.64.1 (running) but not 0.64.2, which the compose file names.
func TestLocalDigest_UpgradedTagNotOnHostIsNotApplied(t *testing.T) {
	f := &fakeLocalImages{
		running: map[string]string{"navidrome|navidrome": "sha256:old"},
		digests: map[string]string{"sha256:old": "sha256:A"},
		refs:    []string{"deluan/navidrome:0.64.1"},
	}
	_, err := lookupWith(f)(context.Background(), "one/navidrome", "navidrome", "deluan/navidrome:0.64.2")
	if !errors.Is(err, images.ErrNotApplied) {
		t.Fatalf("expected ErrNotApplied, got %v", err)
	}
}

func TestLocalDigest_NotRunningAndNotOnHostIsNotApplied(t *testing.T) {
	f := &fakeLocalImages{refs: []string{"deluan/navidrome:0.64.1"}}
	_, err := lookupWith(f)(context.Background(), "one/navidrome", "navidrome", "deluan/navidrome:0.64.2")
	if !errors.Is(err, images.ErrNotApplied) {
		t.Fatalf("expected ErrNotApplied, got %v", err)
	}
}

// Pulled but not recreated: the host has the compose image, the container
// still runs the old one. That stays digest drift (pull --recreate fixes it).
func TestLocalDigest_PulledButNotRecreatedComparesRunningImage(t *testing.T) {
	f := &fakeLocalImages{
		running: map[string]string{"navidrome|navidrome": "sha256:old"},
		digests: map[string]string{"sha256:old": "sha256:A"},
		refs:    []string{"deluan/navidrome:0.64.2"},
	}
	digest, err := lookupWith(f)(context.Background(), "one/navidrome", "navidrome", "deluan/navidrome:0.64.2")
	if err != nil || digest != "sha256:A" {
		t.Fatalf("expected the running image's digest, got %q, %v", digest, err)
	}
}

func TestLocalDigest_NoContainerFallsBackToStoredImage(t *testing.T) {
	f := &fakeLocalImages{
		refs:      []string{"redis:8"},
		inspected: map[string]string{"docker.io/library/redis:8": "sha256:R"},
	}
	digest, err := lookupWith(f)(context.Background(), "one/navidrome", "cache", "docker.io/library/redis:8")
	if err != nil || digest != "sha256:R" {
		t.Fatalf("expected the stored image's digest, got %q, %v", digest, err)
	}
}

// If image ls fails, there is no evidence either way: don't claim not applied.
func TestLocalDigest_ImageListFailureSkipsTheCheck(t *testing.T) {
	f := &fakeLocalImages{refsErr: errors.New("ssh: session refused")}
	_, err := lookupWith(f)(context.Background(), "one/navidrome", "navidrome", "deluan/navidrome:0.64.2")
	if errors.Is(err, images.ErrNotApplied) {
		t.Fatal("a failed image listing must not mark the service not applied")
	}
}

func TestLocalRefKey(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"deluan/navidrome:0.64.2", "deluan/navidrome:0.64.2", true},
		{"redis", "redis:latest", true},
		{"redis:8.10-alpine", "redis:8.10-alpine", true},
		{"library/redis:8", "redis:8", true},
		{"docker.io/library/redis:8", "redis:8", true},
		{"index.docker.io/deluan/navidrome:1", "deluan/navidrome:1", true},
		{"ghcr.io/acme/app:v1", "ghcr.io/acme/app:v1", true},
		{"registry.example.com:5000/app", "registry.example.com:5000/app:latest", true},
		{"registry.example.com:5000/library/app:1", "registry.example.com:5000/library/app:1", true},
		{"nginx@sha256:abc", "", false},
		{"", "", false},
	} {
		got, ok := localRefKey(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("localRefKey(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
