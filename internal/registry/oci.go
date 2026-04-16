package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
)

// manifestAcceptHeader lists the media types the client accepts when querying manifests.
// Includes OCI image index and Docker manifest list to support multi-arch images.
const manifestAcceptHeader = "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json"

// OCIClient implements Registry using the OCI Distribution Spec HTTP API.
type OCIClient struct {
	client *http.Client
	cache  *tokenCache
}

// NewOCIClient creates a new OCI registry client with the given HTTP client.
// If httpClient is nil, http.DefaultClient is used.
func NewOCIClient(httpClient *http.Client) *OCIClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OCIClient{
		client: httpClient,
		cache:  newTokenCache(),
	}
}

// tagsResponse is the JSON structure returned by the /v2/{name}/tags/list endpoint.
type tagsResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// ListTags returns all tags for an image from the remote registry.
// It handles pagination via the Link header.
func (c *OCIClient) ListTags(ctx context.Context, image ImageRef) ([]string, error) {
	const op = "registry.ListTags"

	baseURL := registryURL(image.Registry)
	url := fmt.Sprintf("%s/v2/%s/tags/list", baseURL, image.FullName())

	var allTags []string

	for url != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, apperr.Wrap(op, apperr.Internal, err, "building tags request")
		}

		resp, err := doWithAuth(ctx, c.client, c.cache, req)
		if err != nil {
			return nil, apperr.Wrap(op, apperr.Unavailable, err, "listing tags for %s", image.FullName())
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == http.StatusNotFound {
			return nil, apperr.New(op, apperr.NotFound, "repository not found: %s", image.FullName())
		}
		if resp.StatusCode != http.StatusOK {
			return nil, apperr.New(op, apperr.External, "unexpected status %d listing tags for %s", resp.StatusCode, image.FullName())
		}

		var tr tagsResponse
		if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
			return nil, apperr.Wrap(op, apperr.External, err, "decoding tags response for %s", image.FullName())
		}

		allTags = append(allTags, tr.Tags...)

		// Check for pagination via Link header.
		url = parseNextLink(resp.Header.Get("Link"), baseURL)
	}

	return allTags, nil
}

// GetRemoteDigest returns the digest of a specific tag from the remote registry.
// It uses a HEAD request to the manifests endpoint and reads the Docker-Content-Digest header.
func (c *OCIClient) GetRemoteDigest(ctx context.Context, image ImageRef, tag string) (string, error) {
	const op = "registry.GetRemoteDigest"

	if tag == "" {
		return "", apperr.New(op, apperr.InvalidInput, "tag cannot be empty")
	}

	baseURL := registryURL(image.Registry)
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", baseURL, image.FullName(), tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", apperr.Wrap(op, apperr.Internal, err, "building manifest request")
	}
	req.Header.Set("Accept", manifestAcceptHeader)

	resp, err := doWithAuth(ctx, c.client, c.cache, req)
	if err != nil {
		return "", apperr.Wrap(op, apperr.Unavailable, err, "fetching digest for %s:%s", image.FullName(), tag)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return "", apperr.New(op, apperr.NotFound, "tag not found: %s:%s", image.FullName(), tag)
	}
	if resp.StatusCode != http.StatusOK {
		return "", apperr.New(op, apperr.External, "unexpected status %d fetching digest for %s:%s", resp.StatusCode, image.FullName(), tag)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", apperr.New(op, apperr.External, "no Docker-Content-Digest header in response for %s:%s", image.FullName(), tag)
	}

	return digest, nil
}

// parseNextLink parses the Link header for pagination.
// Format: </v2/name/tags/list?n=100&last=tag>; rel="next"
func parseNextLink(header, baseURL string) string {
	if header == "" {
		return ""
	}

	for part := range strings.SplitSeq(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		// Extract URL between < and >
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start < 0 || end < 0 || end <= start {
			continue
		}
		link := part[start+1 : end]
		// If the link is relative, prepend the base URL.
		if strings.HasPrefix(link, "/") {
			return baseURL + link
		}
		return link
	}

	return ""
}
