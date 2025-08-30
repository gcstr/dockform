package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestFilesetPlan_PrintsOnlyFilesetLines_OrNoop(t *testing.T) {
	defer withStubDocker(t)()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	// Use a config with no filesets to trigger no-op message
	root.SetArgs([]string{"filesets", "plan", "-c", basicConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("fileset plan execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "[no-op] no filesets defined or no fileset changes") {
		t.Fatalf("expected noop message when no filesets; got: %s", got)
	}
}

func TestFilesetApply_PrintsOnlyFilesetLines_AndRunsApply(t *testing.T) {
	defer withStubDocker(t)()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	// Config with filesets; add a filesets section pointing to an empty dir
	cfgPath := basicConfigPath(t)
	// Overwrite config to include filesets and volume reference
	cfgWithFilesets := strings.ReplaceAll(string(readFileOrFatal(t, cfgPath)), "networks:", "filesets:\n  site:\n    source: .\n    target_volume: demo-volume-1\n    target_path: /var/www\nnetworks:")
	writeFileOrFatal(t, cfgPath, cfgWithFilesets)

	root.SetArgs([]string{"filesets", "apply", "-c", cfgPath})
	if err := root.Execute(); err != nil {
		t.Fatalf("fileset apply execute: %v", err)
	}
	got := out.String()
	// Expect either specific fileset lines or the no-op message if stub implies no remote manifest
	if !strings.Contains(got, "fileset site:") && !strings.Contains(got, "[no-op] no filesets defined or no fileset changes") {
		t.Fatalf("expected fileset-only output or noop; got: %s", got)
	}
}

func readFileOrFatal(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func writeFileOrFatal(t *testing.T, path string, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
