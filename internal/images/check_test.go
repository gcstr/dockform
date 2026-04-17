package images

import (
	"context"
	"fmt"
	"testing"

	"github.com/gcstr/dockform/internal/registry"
)

// mockRegistry implements registry.Registry for testing.
type mockRegistry struct {
	tags    map[string][]string          // fullName -> tags
	digests map[string]map[string]string // fullName -> tag -> digest
	listErr map[string]error             // fullName -> error
	getErr  map[string]error             // "fullName:tag" -> error
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		tags:    make(map[string][]string),
		digests: make(map[string]map[string]string),
		listErr: make(map[string]error),
		getErr:  make(map[string]error),
	}
}

func (m *mockRegistry) ListTags(_ context.Context, image registry.ImageRef) ([]string, error) {
	name := image.FullName()
	if err, ok := m.listErr[name]; ok {
		return nil, err
	}
	return m.tags[name], nil
}

func (m *mockRegistry) GetRemoteDigest(_ context.Context, image registry.ImageRef, tag string) (string, error) {
	name := image.FullName()
	key := name + ":" + tag
	if err, ok := m.getErr[key]; ok {
		return "", err
	}
	if tagMap, ok := m.digests[name]; ok {
		if d, ok := tagMap[tag]; ok {
			return d, nil
		}
	}
	return "", fmt.Errorf("digest not found for %s:%s", name, tag)
}

func (m *mockRegistry) setDigest(fullName, tag, digest string) {
	if m.digests[fullName] == nil {
		m.digests[fullName] = make(map[string]string)
	}
	m.digests[fullName][tag] = digest
}

// mockLocalDigest returns a LocalDigestFunc backed by a simple map keyed by imageRef.
func mockLocalDigest(digests map[string]string) LocalDigestFunc {
	return func(_ context.Context, _, _ string, imageRef string) (string, error) {
		if d, ok := digests[imageRef]; ok {
			return d, nil
		}
		return "", fmt.Errorf("local digest not found for %s", imageRef)
	}
}

func TestCheck_DigestMatch(t *testing.T) {
	reg := newMockRegistry()
	reg.setDigest("library/nginx", "1.25", "sha256:abc123")

	localFn := mockLocalDigest(map[string]string{
		"nginx:1.25": "sha256:abc123",
	})

	inputs := []CheckInput{
		{
			StackKey: "ctx/web",
			Services: map[string]string{"web": "nginx:1.25"},
		},
	}

	results, err := Check(context.Background(), inputs, reg, localFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.DigestStale {
		t.Error("expected DigestStale=false")
	}
	if r.CurrentTag != "1.25" {
		t.Errorf("expected CurrentTag=1.25, got %s", r.CurrentTag)
	}
	if r.Error != "" {
		t.Errorf("expected no error, got %s", r.Error)
	}
}

func TestCheck_DigestMismatch(t *testing.T) {
	reg := newMockRegistry()
	reg.setDigest("library/nginx", "1.25", "sha256:remote999")

	localFn := mockLocalDigest(map[string]string{
		"nginx:1.25": "sha256:local111",
	})

	inputs := []CheckInput{
		{
			StackKey: "ctx/web",
			Services: map[string]string{"web": "nginx:1.25"},
		},
	}

	results, err := Check(context.Background(), inputs, reg, localFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := results[0]
	if !r.DigestStale {
		t.Error("expected DigestStale=true")
	}
}

func TestCheck_TagPatternNewerVersions(t *testing.T) {
	reg := newMockRegistry()
	reg.setDigest("library/nginx", "1.25.0", "sha256:abc")
	reg.tags["library/nginx"] = []string{"1.24.0", "1.25.0", "1.26.0", "1.27.0", "2.0.0", "latest"}

	localFn := mockLocalDigest(map[string]string{
		"nginx:1.25.0": "sha256:abc",
	})

	inputs := []CheckInput{
		{
			StackKey:   "ctx/web",
			TagPattern: `^\d+\.\d+\.\d+$`,
			Services:   map[string]string{"web": "nginx:1.25.0"},
		},
	}

	results, err := Check(context.Background(), inputs, reg, localFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := results[0]
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if len(r.NewerTags) != 3 {
		t.Fatalf("expected 3 newer tags, got %d: %v", len(r.NewerTags), r.NewerTags)
	}
	// Should be sorted descending.
	expected := []string{"2.0.0", "1.27.0", "1.26.0"}
	for i, tag := range expected {
		if r.NewerTags[i] != tag {
			t.Errorf("NewerTags[%d] = %s, want %s", i, r.NewerTags[i], tag)
		}
	}
}

func TestCheck_TagPatternNoNewerVersions(t *testing.T) {
	reg := newMockRegistry()
	reg.setDigest("library/nginx", "2.0.0", "sha256:abc")
	reg.tags["library/nginx"] = []string{"1.24.0", "1.25.0", "2.0.0"}

	localFn := mockLocalDigest(map[string]string{
		"nginx:2.0.0": "sha256:abc",
	})

	inputs := []CheckInput{
		{
			StackKey:   "ctx/web",
			TagPattern: `^\d+\.\d+\.\d+$`,
			Services:   map[string]string{"web": "nginx:2.0.0"},
		},
	}

	results, err := Check(context.Background(), inputs, reg, localFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := results[0]
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if len(r.NewerTags) != 0 {
		t.Errorf("expected no newer tags, got %v", r.NewerTags)
	}
}

func TestCheck_CurrentTagNotSemver(t *testing.T) {
	reg := newMockRegistry()
	reg.setDigest("library/nginx", "latest", "sha256:remote")
	reg.tags["library/nginx"] = []string{"1.24.0", "1.25.0", "latest"}

	localFn := mockLocalDigest(map[string]string{
		"nginx:latest": "sha256:local",
	})

	inputs := []CheckInput{
		{
			StackKey:   "ctx/web",
			TagPattern: `^\d+\.\d+\.\d+$`,
			Services:   map[string]string{"web": "nginx:latest"},
		},
	}

	results, err := Check(context.Background(), inputs, reg, localFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := results[0]
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	// Should fall back to digest-only: no newer tags returned.
	if len(r.NewerTags) != 0 {
		t.Errorf("expected no newer tags for non-semver tag, got %v", r.NewerTags)
	}
	if !r.DigestStale {
		t.Error("expected DigestStale=true")
	}
}

func TestCheck_TagPatternFilters(t *testing.T) {
	reg := newMockRegistry()
	reg.setDigest("library/nginx", "1.25.0", "sha256:abc")
	reg.tags["library/nginx"] = []string{
		"1.24.0", "1.25.0", "1.26.0",
		"1.26.0-alpine", "1.27.0-alpine", // Should be excluded by pattern
		"2.0.0",
	}

	localFn := mockLocalDigest(map[string]string{
		"nginx:1.25.0": "sha256:abc",
	})

	inputs := []CheckInput{
		{
			StackKey:   "ctx/web",
			TagPattern: `^\d+\.\d+\.\d+$`, // Strict semver only, no suffixes
			Services:   map[string]string{"web": "nginx:1.25.0"},
		},
	}

	results, err := Check(context.Background(), inputs, reg, localFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := results[0]
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	// Only strict semver tags newer than 1.25.0: 1.26.0, 2.0.0
	expected := []string{"2.0.0", "1.26.0"}
	if len(r.NewerTags) != len(expected) {
		t.Fatalf("expected %d newer tags, got %d: %v", len(expected), len(r.NewerTags), r.NewerTags)
	}
	for i, tag := range expected {
		if r.NewerTags[i] != tag {
			t.Errorf("NewerTags[%d] = %s, want %s", i, r.NewerTags[i], tag)
		}
	}
}

func TestCheck_ImageParseError(t *testing.T) {
	reg := newMockRegistry()
	localFn := mockLocalDigest(map[string]string{})

	inputs := []CheckInput{
		{
			StackKey: "ctx/web",
			Services: map[string]string{"bad": ""},
		},
	}

	results, err := Check(context.Background(), inputs, reg, localFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := results[0]
	if r.Error == "" {
		t.Error("expected error for empty image ref")
	}
}

func TestCheck_RegistryError(t *testing.T) {
	reg := newMockRegistry()
	reg.getErr["library/nginx:1.25"] = fmt.Errorf("registry unavailable")

	localFn := mockLocalDigest(map[string]string{
		"nginx:1.25": "sha256:abc",
	})

	inputs := []CheckInput{
		{
			StackKey: "ctx/web",
			Services: map[string]string{
				"web":   "nginx:1.25",
				"proxy": "nginx:1.25",
			},
		},
	}

	results, err := Check(context.Background(), inputs, reg, localFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both services reference the same failing image; both should have errors.
	for _, r := range results {
		if r.Error == "" {
			t.Errorf("expected error for service %s", r.Service)
		}
	}
}

func TestCheck_MultipleStacksMixedResults(t *testing.T) {
	reg := newMockRegistry()
	reg.setDigest("library/nginx", "1.25.0", "sha256:remote1")
	reg.setDigest("library/redis", "7.0", "sha256:same")
	reg.tags["library/nginx"] = []string{"1.25.0", "1.26.0"}

	localFn := mockLocalDigest(map[string]string{
		"nginx:1.25.0": "sha256:local1",
		"redis:7.0":    "sha256:same",
	})

	inputs := []CheckInput{
		{
			StackKey:   "ctx1/web",
			TagPattern: `^\d+\.\d+\.\d+$`,
			Services:   map[string]string{"web": "nginx:1.25.0"},
		},
		{
			StackKey: "ctx2/cache",
			Services: map[string]string{"cache": "redis:7.0"},
		},
	}

	results, err := Check(context.Background(), inputs, reg, localFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First result: nginx - stale with newer tags.
	nginx := results[0]
	if nginx.Stack != "ctx1/web" {
		t.Errorf("expected stack ctx1/web, got %s", nginx.Stack)
	}
	if !nginx.DigestStale {
		t.Error("expected nginx DigestStale=true")
	}
	if len(nginx.NewerTags) != 1 || nginx.NewerTags[0] != "1.26.0" {
		t.Errorf("expected newer tags [1.26.0], got %v", nginx.NewerTags)
	}

	// Second result: redis - up to date.
	redis := results[1]
	if redis.Stack != "ctx2/cache" {
		t.Errorf("expected stack ctx2/cache, got %s", redis.Stack)
	}
	if redis.DigestStale {
		t.Error("expected redis DigestStale=false")
	}
	if len(redis.NewerTags) != 0 {
		t.Errorf("expected no newer tags for redis, got %v", redis.NewerTags)
	}
}

func TestCheck_LocalDigestError(t *testing.T) {
	reg := newMockRegistry()
	reg.setDigest("library/nginx", "1.25", "sha256:remote")

	localFn := func(_ context.Context, _, _, _ string) (string, error) {
		return "", fmt.Errorf("image not pulled locally")
	}

	inputs := []CheckInput{
		{
			StackKey: "ctx/web",
			Services: map[string]string{"web": "nginx:1.25"},
		},
	}

	results, err := Check(context.Background(), inputs, reg, localFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := results[0]
	if r.Error == "" {
		t.Error("expected error for local digest failure")
	}
	if r.DigestStale {
		t.Error("DigestStale should be false when check errored")
	}
}

func TestCheck_ListTagsError(t *testing.T) {
	reg := newMockRegistry()
	reg.setDigest("library/nginx", "1.25.0", "sha256:abc")
	reg.listErr["library/nginx"] = fmt.Errorf("rate limited")

	localFn := mockLocalDigest(map[string]string{
		"nginx:1.25.0": "sha256:abc",
	})

	inputs := []CheckInput{
		{
			StackKey:   "ctx/web",
			TagPattern: `^\d+\.\d+\.\d+$`,
			Services:   map[string]string{"web": "nginx:1.25.0"},
		},
	}

	results, err := Check(context.Background(), inputs, reg, localFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := results[0]
	if r.Error == "" {
		t.Error("expected error when ListTags fails")
	}
	// Digest comparison should still have run before the tag error.
	// Since digests match, DigestStale should be false.
	if r.DigestStale {
		t.Error("expected DigestStale=false since digests match")
	}
}

func TestCheck_EmptyInputs(t *testing.T) {
	reg := newMockRegistry()
	localFn := mockLocalDigest(map[string]string{})

	results, err := Check(context.Background(), nil, reg, localFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}
