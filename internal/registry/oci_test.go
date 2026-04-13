package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
)

// newTestServer creates a registry mock server and returns the server plus
// an OCIClient configured to talk to it.
func newTestServer(handler http.Handler) (*httptest.Server, *OCIClient) {
	srv := httptest.NewServer(handler)
	client := NewOCIClient(srv.Client())
	return srv, client
}

// imageForServer returns an ImageRef that points to the given test server.
// It uses the full server URL (including scheme) as the Registry field,
// which registryURL() passes through unchanged.
func imageForServer(srv *httptest.Server, name string) ImageRef {
	return ImageRef{
		Registry:  srv.URL,
		Namespace: "",
		Name:      name,
		Tag:       "latest",
	}
}

func TestOCIClient_ListTags_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/myapp/tags/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tagsResponse{
			Name: "myapp",
			Tags: []string{"1.0", "1.1", "latest"},
		})
	})

	srv, client := newTestServer(mux)
	defer srv.Close()

	ref := imageForServer(srv, "myapp")
	tags, err := client.ListTags(context.Background(), ref)
	if err != nil {
		t.Fatalf("ListTags() error: %v", err)
	}

	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(tags))
	}
	want := []string{"1.0", "1.1", "latest"}
	for i, tag := range tags {
		if tag != want[i] {
			t.Errorf("tag[%d] = %q, want %q", i, tag, want[i])
		}
	}
}

func TestOCIClient_ListTags_Pagination(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/myapp/tags/list", func(w http.ResponseWriter, r *http.Request) {
		last := r.URL.Query().Get("last")
		w.Header().Set("Content-Type", "application/json")

		if last == "" {
			// First page — include Link header for next page.
			w.Header().Set("Link", `</v2/myapp/tags/list?last=tag2>; rel="next"`)
			_ = json.NewEncoder(w).Encode(tagsResponse{
				Name: "myapp",
				Tags: []string{"tag1", "tag2"},
			})
		} else {
			// Second page — no Link header.
			_ = json.NewEncoder(w).Encode(tagsResponse{
				Name: "myapp",
				Tags: []string{"tag3", "tag4"},
			})
		}
	})

	srv, client := newTestServer(mux)
	defer srv.Close()

	ref := imageForServer(srv, "myapp")
	tags, err := client.ListTags(context.Background(), ref)
	if err != nil {
		t.Fatalf("ListTags() error: %v", err)
	}

	if len(tags) != 4 {
		t.Fatalf("expected 4 tags, got %d: %v", len(tags), tags)
	}
	want := []string{"tag1", "tag2", "tag3", "tag4"}
	for i, tag := range tags {
		if tag != want[i] {
			t.Errorf("tag[%d] = %q, want %q", i, tag, want[i])
		}
	}
}

func TestOCIClient_ListTags_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/missing/tags/list", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	srv, client := newTestServer(mux)
	defer srv.Close()

	ref := imageForServer(srv, "missing")
	_, err := client.ListTags(context.Background(), ref)
	if err == nil {
		t.Fatal("expected error for missing repository")
	}
	if !apperr.IsKind(err, apperr.NotFound) {
		t.Errorf("expected NotFound error kind, got: %v", err)
	}
}

func TestOCIClient_GetRemoteDigest_Success(t *testing.T) {
	wantDigest := "sha256:abc123def456"

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/myapp/manifests/1.0", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		accept := r.Header.Get("Accept")
		if !strings.Contains(accept, "application/vnd.docker.distribution.manifest.v2+json") {
			t.Errorf("missing expected Accept header, got: %s", accept)
		}
		w.Header().Set("Docker-Content-Digest", wantDigest)
		w.WriteHeader(http.StatusOK)
	})

	srv, client := newTestServer(mux)
	defer srv.Close()

	ref := imageForServer(srv, "myapp")
	digest, err := client.GetRemoteDigest(context.Background(), ref, "1.0")
	if err != nil {
		t.Fatalf("GetRemoteDigest() error: %v", err)
	}
	if digest != wantDigest {
		t.Errorf("digest = %q, want %q", digest, wantDigest)
	}
}

func TestOCIClient_GetRemoteDigest_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/myapp/manifests/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	srv, client := newTestServer(mux)
	defer srv.Close()

	ref := imageForServer(srv, "myapp")
	_, err := client.GetRemoteDigest(context.Background(), ref, "missing")
	if err == nil {
		t.Fatal("expected error for missing tag")
	}
	if !apperr.IsKind(err, apperr.NotFound) {
		t.Errorf("expected NotFound error kind, got: %v", err)
	}
}

func TestOCIClient_GetRemoteDigest_EmptyTag(t *testing.T) {
	client := NewOCIClient(nil)
	_, err := client.GetRemoteDigest(context.Background(), ImageRef{}, "")
	if err == nil {
		t.Fatal("expected error for empty tag")
	}
	if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Errorf("expected InvalidInput error kind, got: %v", err)
	}
}

func TestOCIClient_AuthFlow(t *testing.T) {
	// Simulate a registry that requires token auth.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse{Token: "test-token-123"})
	}))
	defer tokenSrv.Close()

	callCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/secure/tags/list", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token-123" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer realm="%s",service="test-registry",scope="repository:secure:pull"`,
				tokenSrv.URL,
			))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tagsResponse{
			Name: "secure",
			Tags: []string{"v1"},
		})
	})

	srv, client := newTestServer(mux)
	defer srv.Close()

	ref := imageForServer(srv, "secure")
	tags, err := client.ListTags(context.Background(), ref)
	if err != nil {
		t.Fatalf("ListTags() with auth error: %v", err)
	}
	if len(tags) != 1 || tags[0] != "v1" {
		t.Errorf("unexpected tags: %v", tags)
	}

	// Verify that the flow did an unauthenticated request then an authenticated retry.
	if callCount != 2 {
		t.Errorf("expected 2 calls to registry (unauth + auth), got %d", callCount)
	}
}

func TestOCIClient_AuthFlow_TokenCached(t *testing.T) {
	tokenRequests := 0
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenRequests++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse{Token: "cached-token"})
	}))
	defer tokenSrv.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/cached/tags/list", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer cached-token" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer realm="%s",service="test",scope="repository:cached:pull"`,
				tokenSrv.URL,
			))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tagsResponse{Name: "cached", Tags: []string{"v1"}})
	})

	srv, client := newTestServer(mux)
	defer srv.Close()

	ref := imageForServer(srv, "cached")

	// First call — triggers auth.
	_, err := client.ListTags(context.Background(), ref)
	if err != nil {
		t.Fatalf("first ListTags() error: %v", err)
	}

	// Second call — should use cached token.
	_, err = client.ListTags(context.Background(), ref)
	if err != nil {
		t.Fatalf("second ListTags() error: %v", err)
	}

	if tokenRequests != 1 {
		t.Errorf("expected 1 token request (cached on second call), got %d", tokenRequests)
	}
}

func TestOCIClient_GetRemoteDigest_NoDigestHeader(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/myapp/manifests/bad", func(w http.ResponseWriter, _ *http.Request) {
		// Return 200 but no Docker-Content-Digest header.
		w.WriteHeader(http.StatusOK)
	})

	srv, client := newTestServer(mux)
	defer srv.Close()

	ref := imageForServer(srv, "myapp")
	_, err := client.GetRemoteDigest(context.Background(), ref, "bad")
	if err == nil {
		t.Fatal("expected error for missing digest header")
	}
	if !apperr.IsKind(err, apperr.External) {
		t.Errorf("expected External error kind, got: %v", err)
	}
}

func TestOCIClient_ListTags_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/broken/tags/list", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv, client := newTestServer(mux)
	defer srv.Close()

	ref := imageForServer(srv, "broken")
	_, err := client.ListTags(context.Background(), ref)
	if err == nil {
		t.Fatal("expected error for server error")
	}
	if !apperr.IsKind(err, apperr.External) {
		t.Errorf("expected External error kind, got: %v", err)
	}
}

func TestOCIClient_NetworkError(t *testing.T) {
	// Create a server then immediately close it to simulate network error.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := NewOCIClient(srv.Client())
	srv.Close()

	ref := imageForServer(srv, "unreachable")
	_, err := client.ListTags(context.Background(), ref)
	if err == nil {
		t.Fatal("expected error for network failure")
	}
}

func TestParseNextLink(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		baseURL string
		want    string
	}{
		{
			name:    "relative link",
			header:  `</v2/test/tags/list?last=foo>; rel="next"`,
			baseURL: "https://registry.example.com",
			want:    "https://registry.example.com/v2/test/tags/list?last=foo",
		},
		{
			name:    "absolute link",
			header:  `<https://other.com/v2/test/tags/list?last=foo>; rel="next"`,
			baseURL: "https://registry.example.com",
			want:    "https://other.com/v2/test/tags/list?last=foo",
		},
		{
			name:    "empty header",
			header:  "",
			baseURL: "https://registry.example.com",
			want:    "",
		},
		{
			name:    "no next rel",
			header:  `</v2/test/tags/list?last=foo>; rel="prev"`,
			baseURL: "https://registry.example.com",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNextLink(tt.header, tt.baseURL)
			if got != tt.want {
				t.Errorf("parseNextLink() = %q, want %q", got, tt.want)
			}
		})
	}
}
