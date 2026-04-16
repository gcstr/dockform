package registry

import (
	"context"
	"fmt"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
)

const (
	defaultRegistry  = "registry-1.docker.io"
	defaultNamespace = "library"
	defaultTag       = "latest"
)

// Registry provides read-only access to container image registries.
type Registry interface {
	// ListTags returns all tags for an image from the remote registry.
	ListTags(ctx context.Context, image ImageRef) ([]string, error)

	// GetRemoteDigest returns the digest of a specific tag from the remote registry.
	GetRemoteDigest(ctx context.Context, image ImageRef, tag string) (string, error)
}

// ImageRef represents a parsed container image reference.
type ImageRef struct {
	Registry  string // e.g., "registry-1.docker.io"
	Namespace string // e.g., "library" for official images, "org" for ghcr
	Name      string // e.g., "nginx"
	Tag       string // e.g., "1.25", "latest", "" if untagged
}

// FullName returns the full repository path used in API calls (namespace/name).
func (r ImageRef) FullName() string {
	if r.Namespace == "" {
		return r.Name
	}
	return r.Namespace + "/" + r.Name
}

// String returns the fully qualified image reference.
func (r ImageRef) String() string {
	s := r.Registry + "/" + r.FullName()
	if r.Tag != "" {
		s += ":" + r.Tag
	}
	return s
}

// ParseImageRef parses a raw image reference string into an ImageRef.
//
// It handles the following forms:
//   - nginx                              → registry-1.docker.io/library/nginx:latest
//   - nginx:1.25                         → registry-1.docker.io/library/nginx:1.25
//   - ghcr.io/org/app:v1                 → ghcr.io/org/app:v1
//   - registry.example.com/foo/bar:2.0   → as-is
//   - registry.example.com:5000/foo:1.0  → registry with port
func ParseImageRef(raw string) (ImageRef, error) {
	const op = "registry.ParseImageRef"

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ImageRef{}, apperr.New(op, apperr.InvalidInput, "image reference cannot be empty")
	}

	var ref ImageRef

	// Split off the tag (last colon that is not part of a port).
	// We find the tag by looking at the part after the last slash.
	lastSlash := strings.LastIndex(raw, "/")
	nameAndTag := raw
	if lastSlash >= 0 {
		nameAndTag = raw[lastSlash+1:]
	}

	if idx := strings.LastIndex(nameAndTag, ":"); idx >= 0 {
		ref.Tag = nameAndTag[idx+1:]
		raw = raw[:len(raw)-len(ref.Tag)-1]
	} else {
		ref.Tag = defaultTag
	}

	if ref.Tag == "" {
		return ImageRef{}, apperr.New(op, apperr.InvalidInput, "image tag cannot be empty")
	}

	// Now raw has no tag. Split into components.
	parts := strings.Split(raw, "/")

	switch {
	case len(parts) == 1:
		// Simple name like "nginx"
		ref.Registry = defaultRegistry
		ref.Namespace = defaultNamespace
		ref.Name = parts[0]

	case len(parts) == 2:
		// Could be "library/nginx" (Docker Hub with namespace) or "ghcr.io/app"
		if looksLikeRegistry(parts[0]) {
			// e.g., "ghcr.io/app" — registry without namespace
			ref.Registry = parts[0]
			ref.Namespace = ""
			ref.Name = parts[1]
		} else {
			// e.g., "myorg/myapp" — Docker Hub with org namespace
			ref.Registry = defaultRegistry
			ref.Namespace = parts[0]
			ref.Name = parts[1]
		}

	case len(parts) >= 3:
		// e.g., "ghcr.io/org/app" or "registry.example.com:5000/org/app"
		ref.Registry = parts[0]
		// Everything between the registry and the last part is namespace.
		ref.Namespace = strings.Join(parts[1:len(parts)-1], "/")
		ref.Name = parts[len(parts)-1]

	default:
		return ImageRef{}, apperr.New(op, apperr.InvalidInput, "cannot parse image reference: %s", raw)
	}

	if ref.Name == "" {
		return ImageRef{}, apperr.New(op, apperr.InvalidInput, "image name cannot be empty")
	}

	return ref, nil
}

// looksLikeRegistry returns true if the string looks like a registry hostname
// rather than a simple namespace. A registry hostname contains a dot or a colon
// (for port), e.g., "ghcr.io", "registry.example.com", "localhost:5000".
func looksLikeRegistry(s string) bool {
	return strings.ContainsAny(s, ".:")
}

// registryURL returns the base URL for a registry, handling Docker Hub's
// special index endpoint. If the registry already includes a scheme (e.g.,
// from a test server), it is returned as-is.
func registryURL(registry string) string {
	if strings.HasPrefix(registry, "http://") || strings.HasPrefix(registry, "https://") {
		return strings.TrimRight(registry, "/")
	}
	if registry == defaultRegistry {
		return "https://registry-1.docker.io"
	}
	// Default to HTTPS for all registries.
	return fmt.Sprintf("https://%s", registry)
}
