package imagescmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/images"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

// newTestPrinter returns a Printer backed by a buffer for capturing output.
func newTestPrinter(buf *bytes.Buffer) ui.Printer {
	return ui.StdPrinter{Out: buf, Err: buf}
}

// stripANSI removes ANSI escape codes from a string for portable assertions.
func stripANSI(s string) string {
	return ui.StripANSI(s)
}

// --- renderTerminal tests ---

func TestRenderTerminal_EmptyResults(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)
	renderTerminal(pr, nil)

	got := buf.String()
	if !strings.Contains(got, "No images found.") {
		t.Errorf("expected 'No images found.' for empty results, got: %q", got)
	}
}

func TestRenderTerminal_UpToDate(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)

	results := []images.ImageStatus{
		{
			Stack:       "default/web",
			Service:     "nginx",
			Image:       "nginx:1.25",
			CurrentTag:  "1.25",
			DigestStale: false,
			NewerTags:   nil,
		},
	}
	renderTerminal(pr, results)

	got := stripANSI(buf.String())
	if !strings.Contains(got, "up to date") {
		t.Errorf("expected 'up to date', got: %q", got)
	}
	if !strings.Contains(got, "nginx:1.25") {
		t.Errorf("expected 'nginx:1.25' in output, got: %q", got)
	}
}

func TestRenderTerminal_NewerTagsAvailable(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)

	results := []images.ImageStatus{
		{
			Stack:       "default/web",
			Service:     "nginx",
			Image:       "nginx:1.25",
			CurrentTag:  "1.25",
			DigestStale: false,
			NewerTags:   []string{"1.26", "1.27"},
		},
	}
	renderTerminal(pr, results)

	got := stripANSI(buf.String())
	if !strings.Contains(got, "newer versions:") {
		t.Errorf("expected 'newer versions:' in output, got: %q", got)
	}
	if !strings.Contains(got, "1.26") || !strings.Contains(got, "1.27") {
		t.Errorf("expected newer tags in output, got: %q", got)
	}
}

func TestRenderTerminal_DigestStaleOnly(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)

	results := []images.ImageStatus{
		{
			Stack:       "default/web",
			Service:     "redis",
			Image:       "redis",
			CurrentTag:  "7",
			DigestStale: true,
			NewerTags:   nil,
		},
	}
	renderTerminal(pr, results)

	got := stripANSI(buf.String())
	if !strings.Contains(got, "updated upstream") {
		t.Errorf("expected 'updated upstream' for digest-stale image, got: %q", got)
	}
}

func TestRenderTerminal_ImageWithError(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)

	results := []images.ImageStatus{
		{
			Stack:      "default/web",
			Service:    "app",
			Image:      "myapp",
			CurrentTag: "latest",
			Error:      "registry timeout",
		},
	}
	renderTerminal(pr, results)

	got := stripANSI(buf.String())
	if !strings.Contains(got, "registry timeout") {
		t.Errorf("expected error message in output, got: %q", got)
	}
}

func TestRenderTerminal_MultipleStacksGrouped(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)

	results := []images.ImageStatus{
		{
			Stack:      "default/frontend",
			Service:    "nginx",
			Image:      "nginx:1.25",
			CurrentTag: "1.25",
		},
		{
			Stack:      "default/backend",
			Service:    "api",
			Image:      "myapi",
			CurrentTag: "v2",
		},
	}
	renderTerminal(pr, results)

	got := stripANSI(buf.String())
	if !strings.Contains(got, "Stack: default/frontend") {
		t.Errorf("expected 'Stack: default/frontend' in output, got: %q", got)
	}
	if !strings.Contains(got, "Stack: default/backend") {
		t.Errorf("expected 'Stack: default/backend' in output, got: %q", got)
	}
}

func TestRenderTerminal_SameStackGroupedTogether(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)

	results := []images.ImageStatus{
		{Stack: "default/web", Service: "nginx", Image: "nginx:1.25", CurrentTag: "1.25"},
		{Stack: "default/web", Service: "redis", Image: "redis:7", CurrentTag: "7"},
	}
	renderTerminal(pr, results)

	got := stripANSI(buf.String())
	// "Stack: default/web" should appear exactly once
	count := strings.Count(got, "Stack: default/web")
	if count != 1 {
		t.Errorf("expected stack header to appear once, got %d occurrences in: %q", count, got)
	}
}

// --- renderJSON tests ---

func TestRenderJSON_BasicFields(t *testing.T) {
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	results := []images.ImageStatus{
		{
			Stack:       "default/web",
			Service:     "nginx",
			Image:       "nginx:1.25",
			CurrentTag:  "1.25",
			DigestStale: true,
			NewerTags:   []string{"1.26"},
		},
	}

	if err := renderJSON(cmd, results); err != nil {
		t.Fatalf("renderJSON returned error: %v", err)
	}

	var out []jsonResult
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nraw: %s", err, buf.String())
	}

	if len(out) != 1 {
		t.Fatalf("expected 1 result, got %d", len(out))
	}

	r := out[0]
	if r.Stack != "default/web" {
		t.Errorf("stack: want %q, got %q", "default/web", r.Stack)
	}
	if r.Service != "nginx" {
		t.Errorf("service: want %q, got %q", "nginx", r.Service)
	}
	if r.Image != "nginx:1.25" {
		t.Errorf("image: want %q, got %q", "nginx:1.25", r.Image)
	}
	if r.CurrentTag != "1.25" {
		t.Errorf("current_tag: want %q, got %q", "1.25", r.CurrentTag)
	}
	if !r.DigestChanged {
		t.Errorf("digest_changed: want true, got false")
	}
	if len(r.NewerTags) != 1 || r.NewerTags[0] != "1.26" {
		t.Errorf("newer_tags: want [1.26], got %v", r.NewerTags)
	}
}

func TestRenderJSON_EmptyNewerTagsOmitted(t *testing.T) {
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	results := []images.ImageStatus{
		{
			Stack:      "default/web",
			Service:    "nginx",
			Image:      "nginx:1.25",
			CurrentTag: "1.25",
			NewerTags:  nil,
		},
	}

	if err := renderJSON(cmd, results); err != nil {
		t.Fatalf("renderJSON returned error: %v", err)
	}

	// newer_tags should be omitted from JSON when empty (omitempty)
	raw := buf.String()
	if strings.Contains(raw, "newer_tags") {
		t.Errorf("expected newer_tags to be omitted when nil, but found it in: %s", raw)
	}
}

func TestRenderJSON_ErrorField(t *testing.T) {
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	results := []images.ImageStatus{
		{
			Stack:   "default/web",
			Service: "app",
			Image:   "myapp",
			Error:   "connection refused",
		},
	}

	if err := renderJSON(cmd, results); err != nil {
		t.Fatalf("renderJSON returned error: %v", err)
	}

	var out []jsonResult
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if len(out) != 1 || out[0].Error != "connection refused" {
		t.Errorf("expected error field 'connection refused', got: %+v", out)
	}
}

func TestRenderJSON_EmptyResults(t *testing.T) {
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := renderJSON(cmd, []images.ImageStatus{}); err != nil {
		t.Fatalf("renderJSON returned error: %v", err)
	}

	raw := strings.TrimSpace(buf.String())
	if raw != "[]" {
		t.Errorf("expected '[]' for empty results, got: %s", raw)
	}
}

// --- renderUpgradeTerminal tests ---

func TestRenderUpgradeTerminal_EmptyResults(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)
	renderUpgradeTerminal(pr, nil, nil, nil, false)

	got := buf.String()
	if !strings.Contains(got, "No images found.") {
		t.Errorf("expected 'No images found.', got: %q", got)
	}
}

func TestRenderUpgradeTerminal_AlreadyLatest(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)

	results := []images.ImageStatus{
		{
			Stack:      "default/web",
			Service:    "nginx",
			Image:      "nginx:1.27",
			CurrentTag: "1.27",
			NewerTags:  nil,
		},
	}
	renderUpgradeTerminal(pr, results, nil, map[string][]string{}, false)

	got := stripANSI(buf.String())
	if !strings.Contains(got, "already latest") {
		t.Errorf("expected 'already latest', got: %q", got)
	}
}

func TestRenderUpgradeTerminal_DigestStaleNoTagPattern(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)

	results := []images.ImageStatus{
		{
			Stack:       "default/web",
			Service:     "redis",
			Image:       "redis",
			CurrentTag:  "7",
			DigestStale: true,
			NewerTags:   nil,
		},
	}
	renderUpgradeTerminal(pr, results, nil, map[string][]string{}, false)

	got := stripANSI(buf.String())
	if !strings.Contains(got, "no tag_pattern configured") {
		t.Errorf("expected 'no tag_pattern configured', got: %q", got)
	}
	if !strings.Contains(got, "docker compose pull") {
		t.Errorf("expected 'docker compose pull' hint, got: %q", got)
	}
}

func TestRenderUpgradeTerminal_ImageUpgraded(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)

	results := []images.ImageStatus{
		{
			Stack:      "default/web",
			Service:    "nginx",
			Image:      "nginx:1.25",
			CurrentTag: "1.25",
			NewerTags:  []string{"1.27"},
		},
	}
	changes := []images.FileChange{
		{
			StackKey: "default/web",
			Service:  "nginx",
			File:     "/app/docker-compose.yaml",
			Image:    "nginx",
			OldTag:   "1.25",
			NewTag:   "1.27",
		},
	}
	stackFiles := map[string][]string{
		"default/web": {"/app/docker-compose.yaml"},
	}

	renderUpgradeTerminal(pr, results, changes, stackFiles, false)

	got := stripANSI(buf.String())
	if !strings.Contains(got, "nginx:1.25") {
		t.Errorf("expected old ref 'nginx:1.25', got: %q", got)
	}
	if !strings.Contains(got, "nginx:1.27") {
		t.Errorf("expected new ref 'nginx:1.27', got: %q", got)
	}
	if !strings.Contains(got, "docker-compose.yaml updated") {
		t.Errorf("expected '(docker-compose.yaml updated)', got: %q", got)
	}
}

func TestRenderUpgradeTerminal_DryRun(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)

	results := []images.ImageStatus{
		{
			Stack:      "default/web",
			Service:    "nginx",
			Image:      "nginx:1.25",
			CurrentTag: "1.25",
			NewerTags:  []string{"1.27"},
		},
	}
	stackFiles := map[string][]string{
		"default/web": {"/app/docker-compose.yaml"},
	}

	// No changes (dry run — Upgrade was not called)
	renderUpgradeTerminal(pr, results, nil, stackFiles, true)

	got := stripANSI(buf.String())
	if !strings.Contains(got, "dry run") {
		t.Errorf("expected '(dry run)' in output, got: %q", got)
	}
}

func TestRenderUpgradeTerminal_ImageWithError(t *testing.T) {
	var buf bytes.Buffer
	pr := newTestPrinter(&buf)

	results := []images.ImageStatus{
		{
			Stack:      "default/web",
			Service:    "app",
			Image:      "myapp",
			CurrentTag: "v1",
			Error:      "name unknown",
		},
	}
	renderUpgradeTerminal(pr, results, nil, map[string][]string{}, false)

	got := stripANSI(buf.String())
	if !strings.Contains(got, "name unknown") {
		t.Errorf("expected error message, got: %q", got)
	}
}

// --- buildStackFiles tests ---

func TestBuildStackFiles_AbsolutePaths(t *testing.T) {
	cfg := &manifest.Config{
		Stacks: map[string]manifest.Stack{
			"default/web": {
				RootAbs: "/srv/app",
				Files:   []string{"/srv/app/docker-compose.yaml"},
			},
		},
	}

	result := buildStackFiles(cfg)
	paths, ok := result["default/web"]
	if !ok {
		t.Fatal("expected key 'default/web' in result")
	}
	if len(paths) != 1 || paths[0] != "/srv/app/docker-compose.yaml" {
		t.Errorf("expected [/srv/app/docker-compose.yaml], got %v", paths)
	}
}

func TestBuildStackFiles_RelativePaths(t *testing.T) {
	cfg := &manifest.Config{
		Stacks: map[string]manifest.Stack{
			"default/web": {
				RootAbs: "/srv/app",
				Files:   []string{"docker-compose.yaml", "docker-compose.override.yaml"},
			},
		},
	}

	result := buildStackFiles(cfg)
	paths := result["default/web"]
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d: %v", len(paths), paths)
	}
	if paths[0] != "/srv/app/docker-compose.yaml" {
		t.Errorf("path[0]: want %q, got %q", "/srv/app/docker-compose.yaml", paths[0])
	}
	if paths[1] != "/srv/app/docker-compose.override.yaml" {
		t.Errorf("path[1]: want %q, got %q", "/srv/app/docker-compose.override.yaml", paths[1])
	}
}

func TestBuildStackFiles_MultipleStacks(t *testing.T) {
	cfg := &manifest.Config{
		Stacks: map[string]manifest.Stack{
			"default/web": {
				RootAbs: "/srv/web",
				Files:   []string{"compose.yaml"},
			},
			"default/db": {
				RootAbs: "/srv/db",
				Files:   []string{"compose.yaml"},
			},
		},
	}

	result := buildStackFiles(cfg)
	if len(result) != 2 {
		t.Errorf("expected 2 stacks in result, got %d", len(result))
	}

	webPaths := result["default/web"]
	if len(webPaths) != 1 || webPaths[0] != "/srv/web/compose.yaml" {
		t.Errorf("web paths: want [/srv/web/compose.yaml], got %v", webPaths)
	}

	dbPaths := result["default/db"]
	if len(dbPaths) != 1 || dbPaths[0] != "/srv/db/compose.yaml" {
		t.Errorf("db paths: want [/srv/db/compose.yaml], got %v", dbPaths)
	}
}

func TestBuildStackFiles_EmptyFiles(t *testing.T) {
	cfg := &manifest.Config{
		Stacks: map[string]manifest.Stack{
			"default/web": {
				RootAbs: "/srv/app",
				Files:   []string{},
			},
		},
	}

	result := buildStackFiles(cfg)
	paths := result["default/web"]
	if len(paths) != 0 {
		t.Errorf("expected empty paths, got %v", paths)
	}
}
