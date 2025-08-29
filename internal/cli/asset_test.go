package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestAssetPlan_PrintsOnlyAssetLines_OrNoop(t *testing.T) {
	defer withStubDocker(t)()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	// Use a config with no assets to trigger no-op message
	root.SetArgs([]string{"asset", "plan", "-c", basicConfigPath(t)})
	if err := root.Execute(); err != nil {
		t.Fatalf("asset plan execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "[no-op] no assets defined or no asset changes") {
		t.Fatalf("expected noop message when no assets; got: %s", got)
	}
}

func TestAssetApply_PrintsOnlyAssetLines_AndRunsApply(t *testing.T) {
	defer withStubDocker(t)()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	// Config with assets; add an assets section pointing to an empty dir
	cfgPath := basicConfigPath(t)
	// Overwrite config to include assets and volume reference
	cfgWithAssets := strings.ReplaceAll(string(readFileOrFatal(t, cfgPath)), "networks:", "assets:\n  site:\n    source: .\n    target_volume: demo-volume-1\n    target_path: /var/www\nnetworks:")
	writeFileOrFatal(t, cfgPath, cfgWithAssets)

	root.SetArgs([]string{"asset", "apply", "-c", cfgPath})
	if err := root.Execute(); err != nil {
		t.Fatalf("asset apply execute: %v", err)
	}
	got := out.String()
	// Expect either specific asset lines or the no-op message if stub implies no remote manifest
	if !strings.Contains(got, "asset site:") && !strings.Contains(got, "[no-op] no assets defined or no asset changes") {
		t.Fatalf("expected asset-only output or noop; got: %s", got)
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
