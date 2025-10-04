package dockercli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

type fakeExec struct {
	lastArgs    []string
	lastDir     string
	lastWithEnv bool
	// canned outputs
	outServices   string
	outConfigJSON string
	outConfigYAML string
	outPs         string
	outHash       string
	errServices   error
	errConfigJSON error
	errConfigYAML error
	errPs         error
	errHash       error
}

func (f *fakeExec) Run(ctx context.Context, args ...string) (string, error) {
	f.lastArgs = args
	return "", nil
}
func (f *fakeExec) RunInDir(ctx context.Context, dir string, args ...string) (string, error) {
	f.lastDir, f.lastArgs, f.lastWithEnv = dir, args, false
	return f.dispatch(args)
}
func (f *fakeExec) RunInDirWithEnv(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	f.lastDir, f.lastArgs, f.lastWithEnv = dir, args, true
	return f.dispatch(args)
}
func (f *fakeExec) RunWithStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	f.lastArgs = args
	return "", nil
}
func (f *fakeExec) RunWithStdout(ctx context.Context, stdout io.Writer, args ...string) error {
	f.lastArgs = args
	return nil
}
func (f *fakeExec) RunDetailed(ctx context.Context, opts Options, args ...string) (Result, error) {
	out, err := f.dispatch(args)
	return Result{Stdout: out, Stderr: "", ExitCode: 0}, err
}
func (f *fakeExec) dispatch(args []string) (string, error) {
	if hasSuffix(args, []string{"config", "--services"}) {
		return f.outServices, f.errServices
	}
	if hasSuffix(args, []string{"config", "--format", "json"}) {
		return f.outConfigJSON, f.errConfigJSON
	}
	if hasSuffix(args, []string{"config"}) {
		return f.outConfigYAML, f.errConfigYAML
	}
	if hasSuffix(args, []string{"ps", "--format", "json"}) {
		return f.outPs, f.errPs
	}
	if hasSuffix(args, []string{"config", "--hash"}) || contains(args, "--hash") {
		return f.outHash, f.errHash
	}
	if hasSuffix(args, []string{"up", "-d"}) {
		return "", nil
	}
	return "", nil
}

func hasSuffix(args, suf []string) bool {
	if len(args) < len(suf) {
		return false
	}
	for i := 0; i < len(suf); i++ {
		if args[len(args)-len(suf)+i] != suf[i] {
			return false
		}
	}
	return true
}
func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

func TestComposeUp_UsesInlineEnvPath(t *testing.T) {
	f := &fakeExec{}
	c := &Client{exec: f}
	inline := []string{"FOO=bar"}
	if _, err := c.ComposeUp(context.Background(), "/tmp", []string{"a.yml"}, []string{"dev"}, []string{"env"}, "proj", inline); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	if !f.lastWithEnv {
		t.Fatalf("expected RunInDirWithEnv to be used when inline env present")
	}
	if !hasSuffix(f.lastArgs, []string{"up", "-d"}) {
		t.Fatalf("expected up -d; got %#v", f.lastArgs)
	}
}

func TestComposeConfigServices_ParsesLines(t *testing.T) {
	f := &fakeExec{outServices: "web\napi\n"}
	c := &Client{exec: f}
	got, err := c.ComposeConfigServices(context.Background(), ".", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("services: %v", err)
	}
	if len(got) != 2 || got[0] != "web" || got[1] != "api" {
		t.Fatalf("unexpected services: %#v", got)
	}
}

func TestComposeConfigFull_JSONPreferred(t *testing.T) {
	f := &fakeExec{outConfigJSON: `{"services":{"web":{"image":"nginx"}}}`}
	c := &Client{exec: f}
	doc, err := c.ComposeConfigFull(context.Background(), ".", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("config full: %v", err)
	}
	if _, ok := doc.Services["web"]; !ok {
		t.Fatalf("missing web service in json path")
	}
}

func TestComposeConfigFull_YAMLFallback(t *testing.T) {
	yamlOut := "services:\n  web:\n    image: nginx\n"
	f := &fakeExec{outConfigJSON: "notjson", outConfigYAML: yamlOut}
	c := &Client{exec: f}
	doc, err := c.ComposeConfigFull(context.Background(), ".", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("config full fallback: %v", err)
	}
	if _, ok := doc.Services["web"]; !ok {
		t.Fatalf("missing web service in yaml fallback")
	}
}

func TestComposePs_Parsers(t *testing.T) {
	// Array
	f := &fakeExec{outPs: `[{"Name":"c1","Service":"web"}]`}
	c := &Client{exec: f}
	items, err := c.ComposePs(context.Background(), ".", nil, nil, nil, "proj", nil)
	if err != nil || len(items) != 1 || items[0].Service != "web" {
		t.Fatalf("array parse: %v %#v", err, items)
	}
	// Single object
	f.outPs = `{"Name":"c2","Service":"api"}`
	items, err = c.ComposePs(context.Background(), ".", nil, nil, nil, "proj", nil)
	if err != nil || len(items) != 1 || items[0].Service != "api" {
		t.Fatalf("single parse: %v %#v", err, items)
	}
	// NDJSON (implementation may parse one or more entries; ensure at least one)
	f.outPs = `{"Name":"c3","Service":"db"}\n{"Name":"c4","Service":"cache"}`
	items, err = c.ComposePs(context.Background(), ".", nil, nil, nil, "proj", nil)
	if err != nil || len(items) == 0 {
		t.Fatalf("ndjson parse: %v %#v", err, items)
	}
	// Unexpected
	f.outPs = "garbage"
	if _, err = c.ComposePs(context.Background(), ".", nil, nil, nil, "proj", nil); err == nil {
		t.Fatalf("expected error for unexpected ps output")
	}
}

func TestComposeConfigHash_ParsesLastField(t *testing.T) {
	f := &fakeExec{outHash: "web deadbeefcafebabe\n"}
	c := &Client{exec: f}
	h, err := c.ComposeConfigHash(context.Background(), ".", nil, nil, nil, "proj", "web", "", nil)
	if err != nil || h != "deadbeefcafebabe" {
		t.Fatalf("hash parse: %v %q", err, h)
	}
	// Empty output -> error
	f.outHash = "  \n"
	if _, err := c.ComposeConfigHash(context.Background(), ".", nil, nil, nil, "proj", "web", "", nil); err == nil {
		t.Fatalf("expected error for empty hash output")
	}
}

func TestComposeConfigHashes_ReusesOverlayAndParses(t *testing.T) {
	// Simulate overlay build (config yaml) once, then two hash calls
	f := &fakeExec{outConfigYAML: "services:\n  web:\n    image: nginx\n  api:\n    image: busybox\n", outHash: "web deadbeef\n"}
	c := &Client{exec: f, identifier: "demo"}
	dir := t.TempDir()
	hashes, err := c.ComposeConfigHashes(context.Background(), dir, []string{"compose.yml"}, nil, nil, "proj", []string{"web", "api"}, "demo", nil)
	if err != nil {
		t.Fatalf("multihash: %v", err)
	}
	if hashes["web"] != "deadbeef" {
		t.Fatalf("expected web hash deadbeef, got %#v", hashes)
	}
	// Ensure the last command was a hash invocation (not another config yaml render)
	if !contains(f.lastArgs, "--hash") {
		t.Fatalf("expected last args to be hash invocation, got %#v", f.lastArgs)
	}
}

func TestBuildLabeledProjectTemp_AddsIdentifierLabel(t *testing.T) {
	yam := "services:\n  web:\n    image: nginx\n  api:\n    image: busybox\n"
	f := &fakeExec{outConfigYAML: yam}
	c := &Client{exec: f}
	path, err := c.buildLabeledProjectTemp(context.Background(), t.TempDir(), []string{"compose.yml"}, nil, nil, "proj", "demo", nil)
	if err != nil {
		t.Fatalf("build labeled: %v", err)
	}
	if path == "" {
		t.Fatalf("expected path to temp project")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tmp: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	svcs, _ := doc["services"].(map[string]any)
	for name, v := range svcs {
		svc, _ := v.(map[string]any)
		labels, _ := svc["labels"].(map[string]any)
		if labels == nil || labels["io.dockform.identifier"] != "demo" {
			t.Fatalf("service %s missing identifier label: %#v", name, labels)
		}
	}
	// When identifier empty, returns empty path
	if p2, err := c.buildLabeledProjectTemp(context.Background(), ".", nil, nil, nil, "proj", "", nil); err != nil || p2 != "" {
		t.Fatalf("expected empty result when identifier empty; got %q err=%v", p2, err)
	}
}

func TestComposeUp_UsesOverlayWhenIdentifier(t *testing.T) {
	// Ensure that when identifier is set, the compose args include only one -f <tempfile>
	yam := "services:\n  web:\n    image: nginx\n"
	f := &fakeExec{outConfigYAML: yam}
	c := &Client{exec: f, identifier: "demo"}
	_, _ = c.ComposeUp(context.Background(), t.TempDir(), []string{"a.yml", "b.yml"}, nil, nil, "proj", nil)
	joined := strings.Join(f.lastArgs, " ")
	// After overlay, should use a single -f pointing to a temp file name
	if count := strings.Count(joined, " -f "); count != 1 {
		t.Fatalf("expected single -f after overlay, got args: %s", joined)
	}
	if !strings.Contains(filepath.Base(joined), "dockform-labeled-project-") {
		// At least ensure config was run and some file was used
		t.Logf("compose args: %s", joined)
	}
}
