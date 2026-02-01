package manifest

import (
	"testing"
)

func TestDetectBindMounts(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name: "relative path with dot slash",
			content: `services:
  app:
    volumes:
      - ./config:/app/config
      - ./data:/app/data`,
			expected: []string{"./config", "./data"},
		},
		{
			name: "relative path with parent directory",
			content: `services:
  app:
    volumes:
      - ../shared:/app/shared`,
			expected: []string{"../shared"},
		},
		{
			name: "home directory relative path",
			content: `services:
  app:
    volumes:
      - ~/mydata:/app/data`,
			expected: []string{"~/mydata"},
		},
		{
			name: "mixed named volumes and bind mounts",
			content: `services:
  app:
    volumes:
      - myvolume:/app/vol
      - ./config:/app/config
      - /absolute/path:/app/abs`,
			expected: []string{"./config"},
		},
		{
			name: "no bind mounts - only named volumes",
			content: `services:
  app:
    volumes:
      - myvolume:/app/vol
      - othervolume:/app/other`,
			expected: []string{},
		},
		{
			name: "no bind mounts - only absolute paths",
			content: `services:
  app:
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /etc/config:/app/config`,
			expected: []string{},
		},
		{
			name: "bind mounts with extra whitespace",
			content: `services:
  app:
    volumes:
      -    ./config:/app/config
      -  ../data:/app/data  `,
			expected: []string{"./config", "../data"},
		},
		{
			name: "duplicate bind mounts",
			content: `services:
  app:
    volumes:
      - ./config:/app/config
  app2:
    volumes:
      - ./config:/other/path`,
			expected: []string{"./config"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectBindMounts(tt.content)

			if len(result) != len(tt.expected) {
				t.Errorf("detectBindMounts() returned %d mounts, expected %d\nGot: %v\nExpected: %v",
					len(result), len(tt.expected), result, tt.expected)
				return
			}

			// Convert to map for easy comparison (order doesn't matter)
			resultMap := make(map[string]bool)
			for _, m := range result {
				resultMap[m] = true
			}

			for _, expected := range tt.expected {
				if !resultMap[expected] {
					t.Errorf("detectBindMounts() missing expected mount: %s\nGot: %v",
						expected, result)
				}
			}
		})
	}
}
