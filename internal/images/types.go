package images

import "context"

// ImageStatus represents the check result for a single image.
type ImageStatus struct {
	Stack         string   // Stack key (e.g., "hetzner/traefik")
	Service       string   // Service name within the compose file
	Image         string   // Full image reference as written in compose
	CurrentTag    string   // Current tag
	DigestStale   bool     // True if remote digest differs from local
	NewerTags     []string // Newer semver tags (empty if no tag_pattern or no newer tags)
	HasTagPattern bool     // True if a dockform.tag_pattern label is set on this service
	Error         string   // Non-empty if check failed for this image
}

// CheckInput bundles everything needed to check images for a stack.
type CheckInput struct {
	StackKey string                 // Stack key (e.g., "hetzner/traefik")
	Services map[string]ServiceSpec // service name -> image + per-service tag pattern
}

// ServiceSpec holds the image reference and optional tag pattern for a single
// service within a stack. The tag pattern is read from the service's
// `dockform.tag_pattern` compose label; empty means digest-only.
type ServiceSpec struct {
	Image      string
	TagPattern string
}

// LocalDigestFunc returns the local digest for an image reference on the
// Docker daemon associated with the given stack and service.
// Implementations should prefer the digest of the running container (so that
// a pulled-but-not-recreated container still appears stale), falling back to
// the stored image digest when no container is running.
// This is injected to avoid coupling to the docker CLI directly.
type LocalDigestFunc func(ctx context.Context, stackKey, service, imageRef string) (string, error)

// FileChange represents a tag rewrite in a compose file.
type FileChange struct {
	StackKey string // Stack key (e.g., "hetzner/traefik")
	Service  string // Service name
	File     string // Path to compose file that was modified
	Image    string // Image name (without tag)
	OldTag   string // Previous tag
	NewTag   string // New tag written
}
