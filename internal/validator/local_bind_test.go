package validator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

// withComposeConfigStub puts a stub docker on PATH that answers any
// `compose ... config` call with doc, the way `compose config --format json`
// would, and succeeds silently at everything else.
func withComposeConfigStub(t *testing.T, doc string) {
	t.Helper()
	dir := t.TempDir()
	docPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(docPath, []byte(doc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	stub := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = config ]; then cat '" + docPath + "'; exit 0; fi\n" +
		"done\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// loadStack writes a one-stack manifest on contextName and loads it. The raw
// compose file has no volumes, so the load-time check passes and only the
// resolved document the stub serves decides the outcome.
func loadStack(t *testing.T, contextName string) (manifest.Config, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "web")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "compose.yaml"), []byte("services:\n  web:\n    image: nginx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yml := fmt.Sprintf("identifier: test-id\ncontexts:\n  %s: {}\nstacks:\n  %s/web:\n    root: web\n    files:\n      - compose.yaml\n", contextName, contextName)
	if err := os.WriteFile(filepath.Join(dir, "dockform.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg, root
}

// volume renders one resolved compose volume entry.
func volume(typ, source string) string {
	return fmt.Sprintf(`{"type":%q,"source":%q,"target":"/x"}`, typ, source)
}

// composeDoc renders a resolved compose document with one service.
func composeDoc(volumes ...string) string {
	return `{"name":"web","services":{"web":{"image":"nginx","volumes":[` + strings.Join(volumes, ",") + `]}}}`
}

func TestValidate_RemoteContextRejectsABindIntoTheProject(t *testing.T) {
	cfg, root := loadStack(t, "prod")
	src := filepath.Join(root, "cfg")
	withComposeConfigStub(t, composeDoc(volume("bind", src)))

	err := Validate(context.Background(), cfg, dockercli.NewClientFactory())
	if err == nil {
		t.Fatal("expected a remote context to reject a bind source inside the project")
	}
	msg := apperr.DeepestMessage(err)
	for _, want := range []string{"prod/web", src, "empty directory"} {
		if !strings.Contains(msg, want) {
			t.Errorf("printed message %q lacks %q", msg, want)
		}
	}
}

func TestValidate_LocalContextAllowsTheSameBind(t *testing.T) {
	cfg, root := loadStack(t, "default")
	withComposeConfigStub(t, composeDoc(volume("bind", filepath.Join(root, "cfg"))))

	if err := Validate(context.Background(), cfg, dockercli.NewClientFactory()); err != nil {
		t.Fatalf("a local daemon can bind project paths; got %v", err)
	}
}

func TestValidate_RemoteContextAllowsDaemonPaths(t *testing.T) {
	cfg, _ := loadStack(t, "prod")
	withComposeConfigStub(t, composeDoc(volume("bind", "/var/run/docker.sock"), volume("bind", "/srv/data")))

	if err := Validate(context.Background(), cfg, dockercli.NewClientFactory()); err != nil {
		t.Fatalf("paths on the daemon's own filesystem are legitimate; got %v", err)
	}
}

// Only bind mounts carry a host path; a named volume's source is a volume
// name, so it must never be judged as a path even if it looks like one.
func TestValidate_RemoteContextIgnoresNonBindVolumes(t *testing.T) {
	cfg, root := loadStack(t, "prod")
	withComposeConfigStub(t, composeDoc(volume("volume", filepath.Join(root, "cfg"))))

	if err := Validate(context.Background(), cfg, dockercli.NewClientFactory()); err != nil {
		t.Fatalf("a named volume is not a bind mount; got %v", err)
	}
}
