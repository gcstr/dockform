package imagescmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/images"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

var confirmStale = []images.ImageStatus{
	{Stack: "prod/web", Service: "nginx", Image: "nginx:1.27", DigestStale: true},
	{Stack: "prod/web", Service: "cache", Image: "redis:7", DigestStale: true},
	{Stack: "prod/worker", Service: "jobs", Image: "app/jobs:2", DigestStale: true},
}

// runConfirmAndPull drives confirmAndPull with stdin set to input (non-TTY,
// like CI) and reports whether the pull ran and what was printed.
func runConfirmAndPull(t *testing.T, input string, recreate, skipConfirm bool) (pulled bool, output string) {
	t.Helper()
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	pr := ui.StdPrinter{Out: &buf, Err: &buf}

	err := confirmAndPull(cmd, pr, confirmStale, recreate, skipConfirm, func() error {
		pulled = true
		return nil
	})
	if err != nil {
		t.Fatalf("confirmAndPull: %v", err)
	}
	return pulled, stripANSI(buf.String())
}

func TestConfirmAndPull_RecreateDeclinedPullsNothing(t *testing.T) {
	pulled, out := runConfirmAndPull(t, "no\n", true, false)
	if pulled {
		t.Fatal("declining the prompt must not pull or recreate anything")
	}
	if !strings.Contains(out, "canceled") {
		t.Errorf("expected the prompt to report it was canceled, got: %q", out)
	}
}

func TestConfirmAndPull_RecreateNoInputPullsNothing(t *testing.T) {
	// CI with nothing on stdin behaves like apply: no answer means no.
	if pulled, _ := runConfirmAndPull(t, "", true, false); pulled {
		t.Fatal("an empty answer must not pull or recreate anything")
	}
}

func TestConfirmAndPull_RecreateConfirmedPulls(t *testing.T) {
	pulled, out := runConfirmAndPull(t, "yes\n", true, false)
	if !pulled {
		t.Fatal("answering yes should pull and recreate")
	}
	if !strings.Contains(out, "recreate the services listed above") {
		t.Errorf("expected the recreate prompt, got: %q", out)
	}
}

func TestConfirmAndPull_SkipConfirmationDoesNotPrompt(t *testing.T) {
	pulled, out := runConfirmAndPull(t, "", true, true)
	if !pulled {
		t.Fatal("--skip-confirmation should proceed without an answer")
	}
	if strings.Contains(out, "Type yes to confirm") {
		t.Errorf("--skip-confirmation must not prompt, got: %q", out)
	}
	if !strings.Contains(out, "nginx: nginx:1.27") {
		t.Errorf("the preview should still list what gets recreated, got: %q", out)
	}
}

func TestConfirmAndPull_PlainPullNeverPrompts(t *testing.T) {
	pulled, out := runConfirmAndPull(t, "", false, false)
	if !pulled {
		t.Fatal("a plain pull should run without asking")
	}
	if strings.Contains(out, "Type yes to confirm") {
		t.Errorf("a plain pull must not prompt, got: %q", out)
	}
}

func TestRenderPullPreview_NamesEveryServiceToRecreate(t *testing.T) {
	var buf bytes.Buffer
	renderPullPreview(newTestPrinter(&buf), confirmStale, true, false)
	out := stripANSI(buf.String())

	for _, want := range []string{"prod/web", "nginx: nginx:1.27", "cache: redis:7", "prod/worker", "jobs: app/jobs:2", "will be recreated"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q, got: %q", want, out)
		}
	}
	if strings.Contains(out, "dry run") {
		t.Errorf("a real run's preview must not say dry run, got: %q", out)
	}
}

func TestRenderPullPreview_DryRun(t *testing.T) {
	var buf bytes.Buffer
	renderPullPreview(newTestPrinter(&buf), confirmStale, true, true)
	out := stripANSI(buf.String())
	if !strings.Contains(out, "3 image(s) with digest drift (dry run)") || !strings.Contains(out, "would be recreated") {
		t.Errorf("unexpected dry-run preview: %q", out)
	}
}

func TestRenderPullTerminal_RecreateOnlySummarizes(t *testing.T) {
	var buf bytes.Buffer
	renderPullTerminal(newTestPrinter(&buf), confirmStale, true)
	out := stripANSI(buf.String())
	if strings.Contains(out, "prod/web") {
		t.Errorf("after --recreate the preview already listed the images; expected only a summary, got: %q", out)
	}
	if !strings.Contains(out, "3 image(s) pulled and containers recreated") {
		t.Errorf("expected the summary line, got: %q", out)
	}
}
