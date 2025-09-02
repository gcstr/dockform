package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func Test_interpolateEnvPlaceholders_AllPresent(t *testing.T) {
	t.Setenv("FOO", "bar")
	t.Setenv("BAZ", "qux")
	in := "a ${FOO} ${BAZ} z"
	out, missing := interpolateEnvPlaceholders(in)
	if out != "a bar qux z" {
		t.Fatalf("unexpected interpolation output: %q", out)
	}
	if missing != nil {
		t.Fatalf("expected nil missing slice, got: %#v", missing)
	}
}

func Test_interpolateEnvPlaceholders_MissingSorted(t *testing.T) {
	// Ensure variables are not set
	os.Unsetenv("A")
	os.Unsetenv("B")
	in := "x ${B} y ${A}"
	out, missing := interpolateEnvPlaceholders(in)
	if out != "x  y " {
		t.Fatalf("unexpected interpolation output: %q", out)
	}
	if len(missing) != 2 || missing[0] != "A" || missing[1] != "B" {
		t.Fatalf("expected missing [A B], got: %#v", missing)
	}
}

func TestRenderWithWarnings_InterpolatesAndReportsMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dockform.yml")
	t.Setenv("FOO", "ok")
	content := "docker:\n  context: ${FOO}\n# ${MISSING}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	got, missing, err := RenderWithWarnings(path)
	if err != nil {
		t.Fatalf("RenderWithWarnings: %v", err)
	}
	if !strings.Contains(got, "context: ok") {
		t.Fatalf("expected interpolated FOO; got: %q", got)
	}
	if len(missing) != 1 || missing[0] != "MISSING" {
		t.Fatalf("expected missing [MISSING], got: %#v", missing)
	}
}
