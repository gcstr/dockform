package images

import "context"

// ImageStatus represents the check result for a single image.
type ImageStatus struct {
	Stack       string   // Stack key (e.g., "hetzner/traefik")
	Service     string   // Service name within the compose file
	Image       string   // Full image reference as written in compose
	CurrentTag  string   // Current tag
	DigestStale bool     // True if remote digest differs from local
	NewerTags   []string // Newer semver tags (empty if no tag_pattern or no newer tags)
	Error       string   // Non-empty if check failed for this image
}

// CheckInput bundles everything needed to check images for a stack.
type CheckInput struct {
	StackKey   string            // Stack key (e.g., "hetzner/traefik")
	TagPattern string            // From stack.Images.TagPattern, empty means digest-only
	Services   map[string]string // service name -> image reference (from ComposeConfigFull)
}

// LocalDigestFunc returns the local digest for an image reference.
// This is injected to avoid coupling to the docker CLI directly.
type LocalDigestFunc func(ctx context.Context, imageRef string) (string, error)
