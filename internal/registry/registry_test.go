package registry

import (
	"testing"
)

func TestParseImageRef(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    ImageRef
		wantErr bool
	}{
		{
			name: "simple image defaults to Docker Hub library",
			raw:  "nginx",
			want: ImageRef{
				Registry:  "registry-1.docker.io",
				Namespace: "library",
				Name:      "nginx",
				Tag:       "latest",
			},
		},
		{
			name: "image with tag",
			raw:  "nginx:1.25",
			want: ImageRef{
				Registry:  "registry-1.docker.io",
				Namespace: "library",
				Name:      "nginx",
				Tag:       "1.25",
			},
		},
		{
			name: "ghcr with org and tag",
			raw:  "ghcr.io/org/app:v1",
			want: ImageRef{
				Registry:  "ghcr.io",
				Namespace: "org",
				Name:      "app",
				Tag:       "v1",
			},
		},
		{
			name: "custom registry with nested path",
			raw:  "registry.example.com/foo/bar:2.0",
			want: ImageRef{
				Registry:  "registry.example.com",
				Namespace: "foo",
				Name:      "bar",
				Tag:       "2.0",
			},
		},
		{
			name: "registry with port",
			raw:  "registry.example.com:5000/foo:1.0",
			want: ImageRef{
				Registry:  "registry.example.com:5000",
				Namespace: "",
				Name:      "foo",
				Tag:       "1.0",
			},
		},
		{
			name: "Docker Hub with org namespace",
			raw:  "myorg/myapp:latest",
			want: ImageRef{
				Registry:  "registry-1.docker.io",
				Namespace: "myorg",
				Name:      "myapp",
				Tag:       "latest",
			},
		},
		{
			name: "Docker Hub org without tag defaults to latest",
			raw:  "myorg/myapp",
			want: ImageRef{
				Registry:  "registry-1.docker.io",
				Namespace: "myorg",
				Name:      "myapp",
				Tag:       "latest",
			},
		},
		{
			name: "registry with port and nested path",
			raw:  "localhost:5000/myns/myimage:dev",
			want: ImageRef{
				Registry:  "localhost:5000",
				Namespace: "myns",
				Name:      "myimage",
				Tag:       "dev",
			},
		},
		{
			name:    "empty string",
			raw:     "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			raw:     "   ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseImageRef(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseImageRef(%q) expected error, got nil", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseImageRef(%q) unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("ParseImageRef(%q)\n  got:  %+v\n  want: %+v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestImageRef_FullName(t *testing.T) {
	tests := []struct {
		ref  ImageRef
		want string
	}{
		{
			ref:  ImageRef{Namespace: "library", Name: "nginx"},
			want: "library/nginx",
		},
		{
			ref:  ImageRef{Namespace: "", Name: "app"},
			want: "app",
		},
		{
			ref:  ImageRef{Namespace: "org/sub", Name: "app"},
			want: "org/sub/app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.ref.FullName()
			if got != tt.want {
				t.Errorf("FullName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestImageRef_String(t *testing.T) {
	ref := ImageRef{
		Registry:  "registry-1.docker.io",
		Namespace: "library",
		Name:      "nginx",
		Tag:       "1.25",
	}
	want := "registry-1.docker.io/library/nginx:1.25"
	got := ref.String()
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
