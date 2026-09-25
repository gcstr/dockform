package imagescmd

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/images"
	"github.com/gcstr/dockform/internal/manifest"
)

// recordingComposeClient captures the inline env passed to each compose call.
type recordingComposeClient struct {
	upInline     []string
	upServices   []string
	pullInline   []string
	configInline []string
	configDoc    dockercli.ComposeConfigDoc
}

func (c *recordingComposeClient) ComposeConfigFull(ctx context.Context, workingDir string, files, profiles, envFiles []string, inlineEnv []string) (dockercli.ComposeConfigDoc, error) {
	c.configInline = inlineEnv
	return c.configDoc, nil
}

func (c *recordingComposeClient) ComposePull(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, services []string, inlineEnv []string) (string, error) {
	c.pullInline = inlineEnv
	return "", nil
}

func (c *recordingComposeClient) ComposeUpServices(ctx context.Context, workingDir string, files, profiles, envFiles []string, projectName string, services []string, inlineEnv []string) (string, error) {
	c.upInline = inlineEnv
	c.upServices = services
	return "", nil
}

// secretStackFixture builds a stack with one plaintext dotenv "sops" secret
// file. With no Age/PGP backend configured, DecryptAndParse reads it as
// plaintext, which exercises the real decrypt-and-parse path without needing
// the sops binary on PATH.
func secretStackFixture(t *testing.T) (*manifest.Config, []images.ImageStatus) {
	t.Helper()

	dir := t.TempDir()
	secretsPath := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(secretsPath, []byte("DATABASE_PASSWORD=s3cret\nSECRET_KEY_BASE=abc123\n"), 0o600); err != nil {
		t.Fatalf("write secrets: %v", err)
	}

	stack := manifest.Stack{
		Root:        dir,
		RootAbs:     dir,
		Files:       []string{"compose.yaml"},
		EnvInline:   []string{"FOO=bar"},
		SopsSecrets: []string{secretsPath},
	}

	cfg := &manifest.Config{
		Stacks: map[string]manifest.Stack{"local/app": stack},
	}
	stale := []images.ImageStatus{
		{Stack: "local/app", Service: "app", Image: "postgis/postgis:17-3.6-alpine", DigestStale: true},
	}
	return cfg, stale
}

func TestExecutePull_RecreateInjectsSopsSecrets(t *testing.T) {
	cfg, stale := secretStackFixture(t)
	client := &recordingComposeClient{}
	get := func(ctxName string) composeClient { return client }

	if err := executePull(context.Background(), stale, cfg.GetAllStacks(), get, cfg, true); err != nil {
		t.Fatalf("executePull: %v", err)
	}

	for _, want := range []string{"FOO=bar", "DATABASE_PASSWORD=s3cret", "SECRET_KEY_BASE=abc123"} {
		if !slices.Contains(client.upInline, want) {
			t.Errorf("ComposeUpServices inline env missing %q; got %v", want, client.upInline)
		}
	}
}

func TestExecutePull_PullUsesSopsSecrets(t *testing.T) {
	cfg, stale := secretStackFixture(t)
	client := &recordingComposeClient{}
	get := func(ctxName string) composeClient { return client }

	if err := executePull(context.Background(), stale, cfg.GetAllStacks(), get, cfg, false); err != nil {
		t.Fatalf("executePull: %v", err)
	}

	if !slices.Contains(client.pullInline, "DATABASE_PASSWORD=s3cret") {
		t.Errorf("ComposePull inline env missing decrypted secret; got %v", client.pullInline)
	}
}

func TestBuildCheckInputs_ConfigUsesSopsSecrets(t *testing.T) {
	cfg, _ := secretStackFixture(t)
	client := &recordingComposeClient{
		configDoc: dockercli.ComposeConfigDoc{
			Services: map[string]dockercli.ComposeService{
				"app": {Image: "postgis/postgis:17-3.6-alpine"},
			},
		},
	}
	get := func(ctxName string) composeClient { return client }

	if _, err := buildCheckInputs(context.Background(), cfg, get); err != nil {
		t.Fatalf("buildCheckInputs: %v", err)
	}

	if !slices.Contains(client.configInline, "DATABASE_PASSWORD=s3cret") {
		t.Errorf("ComposeConfigFull inline env missing decrypted secret; got %v", client.configInline)
	}
}

func TestExecutePull_FailsWhenSecretsCannotBeRead(t *testing.T) {
	cfg, stale := secretStackFixture(t)
	stack := cfg.Stacks["local/app"]
	stack.SopsSecrets = []string{filepath.Join(t.TempDir(), "missing.env")}
	cfg.Stacks["local/app"] = stack

	client := &recordingComposeClient{}
	get := func(ctxName string) composeClient { return client }

	err := executePull(context.Background(), stale, cfg.GetAllStacks(), get, cfg, true)
	if err == nil {
		t.Fatal("expected an error when a stack's secrets cannot be read, got nil")
	}
	if client.upInline != nil {
		t.Errorf("containers must not be recreated with unresolved secrets; ComposeUpServices got %v", client.upInline)
	}
}

func TestExecutePull_RecreateOnlyThePulledServices(t *testing.T) {
	cfg, _ := secretStackFixture(t)
	stale := []images.ImageStatus{
		{Stack: "local/app", Service: "app", Image: "app:1", DigestStale: true},
		{Stack: "local/app", Service: "worker", Image: "worker:1", DigestStale: true},
	}
	client := &recordingComposeClient{}
	get := func(ctxName string) composeClient { return client }

	if err := executePull(context.Background(), stale, cfg.GetAllStacks(), get, cfg, true); err != nil {
		t.Fatalf("executePull: %v", err)
	}
	if !slices.Equal(client.upServices, []string{"app", "worker"}) {
		t.Errorf("compose up should be scoped to the pulled services, got %v", client.upServices)
	}
}
