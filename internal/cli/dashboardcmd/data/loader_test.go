package data

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

type fakeComposeClient struct {
	T               *testing.T
	ExpectWorking   string
	ExpectFiles     []string
	ExpectEnvFiles  []string
	ExpectProfiles  []string
	ExpectInlineEnv []string
	Doc             dockercli.ComposeConfigDoc
	Err             error
	Called          bool
}

func (f *fakeComposeClient) ComposeConfigFull(_ context.Context, workingDir string, files []string, profiles []string, envFiles []string, inline []string) (dockercli.ComposeConfigDoc, error) {
	f.Called = true
	if f.ExpectWorking != "" && workingDir != f.ExpectWorking {
		f.T.Fatalf("expected working dir %q, got %q", f.ExpectWorking, workingDir)
	}
	if f.ExpectFiles != nil && !reflect.DeepEqual(files, f.ExpectFiles) {
		f.T.Fatalf("expected files %v, got %v", f.ExpectFiles, files)
	}
	if f.ExpectEnvFiles != nil && !reflect.DeepEqual(envFiles, f.ExpectEnvFiles) {
		f.T.Fatalf("expected env files %v, got %v", f.ExpectEnvFiles, envFiles)
	}
	if f.ExpectProfiles != nil && !reflect.DeepEqual(profiles, f.ExpectProfiles) {
		f.T.Fatalf("expected profiles %v, got %v", f.ExpectProfiles, profiles)
	}
	if f.ExpectInlineEnv != nil && !reflect.DeepEqual(inline, f.ExpectInlineEnv) {
		f.T.Fatalf("expected inline env %v, got %v", f.ExpectInlineEnv, inline)
	}
	return f.Doc, f.Err
}

func TestNewLoaderNilConfig(t *testing.T) {
	if _, err := NewLoader(nil, nil); err == nil {
		t.Fatalf("expected error when config is nil")
	}
}

func TestStackSummariesBuildsServices(t *testing.T) {
	base := t.TempDir()
	working := filepath.Join(base, "paperless")
	files := []string{filepath.Join(working, "docker-compose.yml")}
	envFiles := []string{filepath.Join(working, "env/.env")}

	fake := &fakeComposeClient{
		T:               t,
		ExpectWorking:   working,
		ExpectFiles:     files,
		ExpectEnvFiles:  envFiles,
		ExpectProfiles:  []string{"default"},
		ExpectInlineEnv: []string{"API_KEY=value"},
		Doc: dockercli.ComposeConfigDoc{Services: map[string]dockercli.ComposeService{
			"paperless-redis": {Image: " redis:8 ", ContainerName: " paperless-redis "},
			"paperless-ngx":   {Image: "", ContainerName: "paperless"},
		}},
	}

	cfg := &manifest.Config{
		BaseDir: base,
		Stacks: map[string]manifest.Stack{
			"paperless": {
				Root:      "paperless",
				Files:     []string{"docker-compose.yml"},
				EnvFile:   []string{"env/.env"},
				Profiles:  []string{"default"},
				EnvInline: []string{"API_KEY=value"},
			},
		},
	}

	loader, err := NewLoader(cfg, fake)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	summaries, err := loader.StackSummaries(context.Background())
	if err != nil {
		t.Fatalf("StackSummaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 stack summary, got %d", len(summaries))
	}
	stack := summaries[0]
	if stack.Name != "paperless" {
		t.Fatalf("expected stack name 'paperless', got %q", stack.Name)
	}
	if len(stack.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(stack.Services))
	}
	first := stack.Services[0]
	if first.Service != "paperless-ngx" {
		t.Fatalf("expected sorted services, first should be paperless-ngx, got %q", first.Service)
	}
	if first.Image != "(no image)" {
		t.Fatalf("expected fallback image '(no image)', got %q", first.Image)
	}
	second := stack.Services[1]
	if second.Service != "paperless-redis" {
		t.Fatalf("expected second service paperless-redis, got %q", second.Service)
	}
	if second.Image != "redis:8" {
		t.Fatalf("expected trimmed image 'redis:8', got %q", second.Image)
	}
	if second.ContainerName != "paperless-redis" {
		t.Fatalf("expected trimmed container name 'paperless-redis', got %q", second.ContainerName)
	}
	if !fake.Called {
		t.Fatalf("expected compose client to be called")
	}
}

func TestStackSummariesErrorsWithoutDocker(t *testing.T) {
	cfg := &manifest.Config{}
	loader, err := NewLoader(cfg, nil)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if _, err := loader.StackSummaries(context.Background()); err == nil {
		t.Fatalf("expected error when docker client is nil")
	}
}

func TestStackSummariesPropagatesComposeError(t *testing.T) {
	base := t.TempDir()
	fake := &fakeComposeClient{
		T:   t,
		Err: errors.New("boom"),
	}
	cfg := &manifest.Config{
		BaseDir: base,
		Stacks:  map[string]manifest.Stack{"app": {}},
	}
	loader, err := NewLoader(cfg, fake)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if _, err := loader.StackSummaries(context.Background()); err == nil {
		t.Fatalf("expected compose error to propagate")
	}
}
