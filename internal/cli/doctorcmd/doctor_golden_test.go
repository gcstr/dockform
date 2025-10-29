package doctorcmd_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
)

var update = flag.Bool("update", false, "update golden files")

func TestDoctorCmd_Golden_Healthy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping golden test on Windows due to output format differences")
	}
	defer withHealthyDoctorStub(t)()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor"})

	_ = root.Execute() // Ignore error, we're testing output format

	got := sanitizeDoctorOutput(out.String())
	goldenPath := filepath.Join("testdata", "doctor", "healthy.golden")

	if *update {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("output mismatch\nwant:\n%s\n---\ngot:\n%s", want, got)
	}
}

func TestDoctorCmd_Golden_EngineUnreachable(t *testing.T) {
	defer withDoctorStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "connection refused" 1>&2
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
`)()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor"})

	_ = root.Execute()

	got := sanitizeDoctorOutput(out.String())
	goldenPath := filepath.Join("testdata", "doctor", "engine_fail.golden")

	if *update {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("output mismatch\nwant:\n%s\n---\ngot:\n%s", want, got)
	}
}

func TestDoctorCmd_Golden_ComposeMissing(t *testing.T) {
	defer withDoctorStub(t, `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "20.0.0"
    exit 0
    ;;
  context)
    echo '"unix:///var/run/docker.sock"'
    exit 0
    ;;
  compose)
    echo "compose: command not found" 1>&2
    exit 1
    ;;
  image)
    exit 0
    ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "create" ]; then exit 0; fi
    if [ "$sub" = "rm" ]; then exit 0; fi
    ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "create" ]; then exit 0; fi
    if [ "$sub" = "rm" ]; then exit 0; fi
    ;;
esac
exit 0
`)()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor"})

	_ = root.Execute()

	got := sanitizeDoctorOutput(out.String())
	goldenPath := filepath.Join("testdata", "doctor", "compose_missing.golden")

	if *update {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("output mismatch\nwant:\n%s\n---\ngot:\n%s", want, got)
	}
}

func TestDoctorCmd_Golden_SopsWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping golden test on Windows due to output format differences")
	}
	// Create a stub with docker but without sops
	stubScript := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  version)
    echo "20.0.0"
    exit 0
    ;;
  context)
    echo '"unix:///var/run/docker.sock"'
    exit 0
    ;;
  compose)
    echo "2.29.0"
    exit 0
    ;;
  image)
    exit 0
    ;;
  network)
    sub="$1"; shift
    if [ "$sub" = "create" ]; then exit 0; fi
    if [ "$sub" = "rm" ]; then exit 0; fi
    ;;
  volume)
    sub="$1"; shift
    if [ "$sub" = "create" ]; then exit 0; fi
    if [ "$sub" = "rm" ]; then exit 0; fi
    ;;
esac
exit 0
`
	dir := t.TempDir()
	dockerPath := filepath.Join(dir, "docker")
	if runtime.GOOS == "windows" {
		dockerPath += ".cmd"
	}
	if err := os.WriteFile(dockerPath, []byte(stubScript), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}

	// Set PATH to only include our stub dir (no sops)
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	defer func() { _ = os.Setenv("PATH", oldPath) }()

	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor"})

	_ = root.Execute()

	got := sanitizeDoctorOutput(out.String())
	goldenPath := filepath.Join("testdata", "doctor", "sops_warning.golden")

	if *update {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if got != string(want) {
		t.Errorf("output mismatch\nwant:\n%s\n---\ngot:\n%s", want, got)
	}
}

// sanitizeDoctorOutput removes dynamic content (timing, error details) for golden comparison
func sanitizeDoctorOutput(output string) string {
	lines := strings.Split(output, "\n")
	var result []string

	// Regex patterns for lines to skip or sanitize
	timingPattern := regexp.MustCompile(`^Completed in .+ • exit code \d+$`)
	errorPattern := regexp.MustCompile(`^│\s+Error: dockercli\.Exec:`)

	for i, line := range lines {
		// Skip timing lines
		if timingPattern.MatchString(line) {
			continue
		}
		// Skip detailed error lines from dockercli.Exec (keep the summary error line)
		if errorPattern.MatchString(line) {
			continue
		}

		// Preserve the blank line after the header (line 2)
		if i < 3 {
			result = append(result, line)
			continue
		}

		// For other lines, preserve structure
		result = append(result, line)
	}

	// Trim trailing empty lines
	for len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
		result = result[:len(result)-1]
	}

	return strings.Join(result, "\n")
}
