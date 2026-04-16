package images

import (
	"os"
	"path/filepath"
	"testing"
)

// writeComposeFile creates a compose file with the given content inside dir.
func writeComposeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeComposeFile: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readFile %s: %v", path, err)
	}
	return string(data)
}

func TestUpgrade_SingleImageRewrite(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  proxy:
    image: traefik:v3.0.1
    ports:
      - "80:80"
`
	path := writeComposeFile(t, dir, "docker-compose.yml", content)

	results := []ImageStatus{
		{
			Stack:      "hetzner/traefik",
			Service:    "proxy",
			Image:      "traefik:v3.0.1",
			CurrentTag: "v3.0.1",
			NewerTags:  []string{"v3.2.1", "v3.1.0"},
		},
	}
	stackFiles := map[string][]string{
		"hetzner/traefik": {path},
	}

	changes, err := Upgrade(results, stackFiles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	c := changes[0]
	if c.StackKey != "hetzner/traefik" {
		t.Errorf("StackKey = %q, want %q", c.StackKey, "hetzner/traefik")
	}
	if c.Service != "proxy" {
		t.Errorf("Service = %q, want %q", c.Service, "proxy")
	}
	if c.Image != "traefik" {
		t.Errorf("Image = %q, want %q", c.Image, "traefik")
	}
	if c.OldTag != "v3.0.1" {
		t.Errorf("OldTag = %q, want %q", c.OldTag, "v3.0.1")
	}
	if c.NewTag != "v3.2.1" {
		t.Errorf("NewTag = %q, want %q", c.NewTag, "v3.2.1")
	}
	if c.File != path {
		t.Errorf("File = %q, want %q", c.File, path)
	}

	got := readFile(t, path)
	if want := `services:
  proxy:
    image: traefik:v3.2.1
    ports:
      - "80:80"
`; got != want {
		t.Errorf("file content mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpgrade_MultipleImagesInSameFile(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  proxy:
    image: traefik:v3.0.1
  app:
    image: myapp:1.0.0
`
	path := writeComposeFile(t, dir, "docker-compose.yml", content)

	results := []ImageStatus{
		{
			Stack:      "mystack",
			Service:    "proxy",
			Image:      "traefik:v3.0.1",
			CurrentTag: "v3.0.1",
			NewerTags:  []string{"v3.2.1"},
		},
		{
			Stack:      "mystack",
			Service:    "app",
			Image:      "myapp:1.0.0",
			CurrentTag: "1.0.0",
			NewerTags:  []string{"1.2.0"},
		},
	}
	stackFiles := map[string][]string{
		"mystack": {path},
	}

	changes, err := Upgrade(results, stackFiles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}

	got := readFile(t, path)
	if want := `services:
  proxy:
    image: traefik:v3.2.1
  app:
    image: myapp:1.2.0
`; got != want {
		t.Errorf("file content mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpgrade_NoQualifyingResults_EmptyNewerTags(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  proxy:
    image: traefik:v3.0.1
`
	path := writeComposeFile(t, dir, "docker-compose.yml", content)

	results := []ImageStatus{
		{
			Stack:      "mystack",
			Service:    "proxy",
			Image:      "traefik:v3.0.1",
			CurrentTag: "v3.0.1",
			NewerTags:  []string{}, // empty — no upgrade available
		},
	}
	stackFiles := map[string][]string{
		"mystack": {path},
	}

	changes, err := Upgrade(results, stackFiles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("expected no changes, got %d", len(changes))
	}

	// File should be untouched.
	got := readFile(t, path)
	if got != content {
		t.Errorf("file should be unchanged, got:\n%s", got)
	}
}

func TestUpgrade_ResultsWithErrorsAreSkipped(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  proxy:
    image: traefik:v3.0.1
`
	path := writeComposeFile(t, dir, "docker-compose.yml", content)

	results := []ImageStatus{
		{
			Stack:      "mystack",
			Service:    "proxy",
			Image:      "traefik:v3.0.1",
			CurrentTag: "v3.0.1",
			NewerTags:  []string{"v3.2.1"},
			Error:      "registry unavailable",
		},
	}
	stackFiles := map[string][]string{
		"mystack": {path},
	}

	changes, err := Upgrade(results, stackFiles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("expected no changes for errored result, got %d", len(changes))
	}

	// File should be untouched.
	got := readFile(t, path)
	if got != content {
		t.Errorf("file should be unchanged, got:\n%s", got)
	}
}

func TestUpgrade_FileNotFound(t *testing.T) {
	results := []ImageStatus{
		{
			Stack:      "mystack",
			Service:    "proxy",
			Image:      "traefik:v3.0.1",
			CurrentTag: "v3.0.1",
			NewerTags:  []string{"v3.2.1"},
		},
	}
	stackFiles := map[string][]string{
		"mystack": {"/nonexistent/path/docker-compose.yml"},
	}

	_, err := Upgrade(results, stackFiles)
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestUpgrade_OriginalFormattingAndCommentsPreserved(t *testing.T) {
	dir := t.TempDir()
	content := `# Managed by dockform
version: "3.9"

services:
  # Reverse proxy
  proxy:
    image: traefik:v3.0.1
    # Important: keep port 80 open
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
`
	path := writeComposeFile(t, dir, "docker-compose.yml", content)

	results := []ImageStatus{
		{
			Stack:      "infra",
			Service:    "proxy",
			Image:      "traefik:v3.0.1",
			CurrentTag: "v3.0.1",
			NewerTags:  []string{"v3.2.1"},
		},
	}
	stackFiles := map[string][]string{
		"infra": {path},
	}

	changes, err := Upgrade(results, stackFiles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	got := readFile(t, path)
	want := `# Managed by dockform
version: "3.9"

services:
  # Reverse proxy
  proxy:
    image: traefik:v3.2.1
    # Important: keep port 80 open
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
`
	if got != want {
		t.Errorf("file content mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpgrade_QuotedImageIsReplaced(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  proxy:
    image: "traefik:v3.0.1"
`
	path := writeComposeFile(t, dir, "docker-compose.yml", content)

	results := []ImageStatus{
		{
			Stack:      "mystack",
			Service:    "proxy",
			Image:      "traefik:v3.0.1",
			CurrentTag: "v3.0.1",
			NewerTags:  []string{"v3.2.1"},
		},
	}
	stackFiles := map[string][]string{
		"mystack": {path},
	}

	changes, err := Upgrade(results, stackFiles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	got := readFile(t, path)
	want := `services:
  proxy:
    image: "traefik:v3.2.1"
`
	if got != want {
		t.Errorf("file content mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpgrade_StackNotInStackFiles(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  proxy:
    image: traefik:v3.0.1
`
	path := writeComposeFile(t, dir, "docker-compose.yml", content)

	results := []ImageStatus{
		{
			Stack:      "other-stack",
			Service:    "proxy",
			Image:      "traefik:v3.0.1",
			CurrentTag: "v3.0.1",
			NewerTags:  []string{"v3.2.1"},
		},
	}
	// stackFiles does not contain "other-stack"
	stackFiles := map[string][]string{
		"mystack": {path},
	}

	changes, err := Upgrade(results, stackFiles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("expected no changes when stack not in stackFiles, got %d", len(changes))
	}
}

func TestUpgrade_ImageNotPresentInFile(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  proxy:
    image: nginx:1.25
`
	path := writeComposeFile(t, dir, "docker-compose.yml", content)

	results := []ImageStatus{
		{
			Stack:      "mystack",
			Service:    "proxy",
			Image:      "traefik:v3.0.1", // not in the file
			CurrentTag: "v3.0.1",
			NewerTags:  []string{"v3.2.1"},
		},
	}
	stackFiles := map[string][]string{
		"mystack": {path},
	}

	changes, err := Upgrade(results, stackFiles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No match → no changes, file untouched.
	if len(changes) != 0 {
		t.Errorf("expected no changes, got %d", len(changes))
	}

	got := readFile(t, path)
	if got != content {
		t.Errorf("file should be unchanged")
	}
}
