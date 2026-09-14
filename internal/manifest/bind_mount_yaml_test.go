package manifest

import "testing"

// TestDetectBindMountsComposeSyntaxes covers the compose volume syntaxes a raw
// regex cannot see. Each row is a way of writing the same relative bind mount
// that dockerd would auto-create as a directory on a remote host.
func TestDetectBindMountsComposeSyntaxes(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name: "short form quoted with double quotes",
			content: `services:
  app:
    volumes:
      - "./config:/app/config"`,
			expected: []string{"./config"},
		},
		{
			name: "short form quoted with single quotes",
			content: `services:
  app:
    volumes:
      - './config:/app/config'`,
			expected: []string{"./config"},
		},
		{
			name: "short form with access mode suffix",
			content: `services:
  app:
    volumes:
      - ./config:/app/config:ro`,
			expected: []string{"./config"},
		},
		{
			name: "short form binding the stack directory itself",
			content: `services:
  app:
    volumes:
      - .:/app`,
			expected: []string{"."},
		},
		{
			name: "long form type bind",
			content: `services:
  app:
    volumes:
      - type: bind
        source: ./config
        target: /app/config`,
			expected: []string{"./config"},
		},
		{
			name: "long form type bind with parent directory",
			content: `services:
  app:
    volumes:
      - type: bind
        source: ../shared
        target: /app/shared`,
			expected: []string{"../shared"},
		},
		{
			name: "long form type volume is not a bind mount",
			content: `services:
  app:
    volumes:
      - type: volume
        source: app_config
        target: /app/config`,
			expected: []string{},
		},
		{
			name: "long form type bind with absolute source is left alone",
			content: `services:
  app:
    volumes:
      - type: bind
        source: /var/run/docker.sock
        target: /var/run/docker.sock`,
			expected: []string{},
		},
		{
			name: "top level volumes block is not scanned",
			content: `services:
  app:
    volumes:
      - app_config:/app/config
volumes:
  app_config:
    external: true`,
			expected: []string{},
		},
		{
			name: "anonymous volume without a host source is not a bind mount",
			content: `services:
  app:
    volumes:
      - /app/cache`,
			expected: []string{},
		},
		{
			name: "mixed syntaxes in one service",
			content: `services:
  app:
    volumes:
      - "./config:/app/config"
      - type: bind
        source: ../shared
        target: /app/shared
      - app_data:/data
      - /var/run/docker.sock:/var/run/docker.sock`,
			expected: []string{"./config", "../shared"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectBindMounts(tt.content)
			if len(got) != len(tt.expected) {
				t.Fatalf("detectBindMounts() = %v (%d), want %v (%d)", got, len(got), tt.expected, len(tt.expected))
			}
			gotSet := make(map[string]bool, len(got))
			for _, m := range got {
				gotSet[m] = true
			}
			for _, want := range tt.expected {
				if !gotSet[want] {
					t.Errorf("detectBindMounts() missing %q; got %v", want, got)
				}
			}
		})
	}
}

// TestDetectBindMountsYAMLAnchors guards the coverage that parsing YAML buys
// over matching raw text. An anchor defined but never referenced is not a
// mount at all; the old regex reported it anyway.
func TestDetectBindMountsYAMLAnchors(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name: "volumes list supplied by an alias",
			content: `x-common-volumes: &common
  - ./config:/app/config
services:
  app:
    volumes: *common`,
			expected: []string{"./config"},
		},
		{
			name: "volumes inherited through a merge key",
			content: `x-common: &common
  volumes:
    - ./config:/app/config
services:
  app:
    <<: *common`,
			expected: []string{"./config"},
		},
		{
			name: "anchor defined but never referenced by a service",
			content: `x-unused: &unused
  volumes:
    - ./orphan:/app/orphan
services:
  app:
    volumes:
      - app_data:/data`,
			expected: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectBindMounts(tt.content)
			if len(got) != len(tt.expected) {
				t.Fatalf("detectBindMounts() = %v (%d), want %v (%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i, want := range tt.expected {
				if got[i] != want {
					t.Errorf("detectBindMounts()[%d] = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}
