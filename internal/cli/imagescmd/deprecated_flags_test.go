package imagescmd_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
)

func TestImagesCheck_SequentialIsDeprecated(t *testing.T) {
	root := cli.TestNewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"images", "check", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("images check --help: %v", err)
	}
	if strings.Contains(out.String(), "--sequential") {
		t.Errorf("--sequential should be hidden from help, got: %q", out.String())
	}

	// Still accepted, so existing scripts keep working, with a warning.
	root = cli.TestNewRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"images", "check", "--sequential", "--manifest", filepath.Join(t.TempDir(), "missing.yml")})
	err := root.Execute()
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("--sequential must still be accepted, got: %v", err)
	}
	if !strings.Contains(out.String(), "--sequential has been deprecated, it has no effect and will be removed") {
		t.Errorf("expected a deprecation warning, got: %q", out.String())
	}
}
