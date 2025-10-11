package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestSpinnerStartStopEnabled(t *testing.T) {
	t.Setenv("DOCKFORM_SPINNER_HIDDEN", "")

	var buf bytes.Buffer
	sp := NewSpinner(&buf, "working")
	// Force-enable for test environment (not a real TTY).
	sp.enabled = true
	sp.frames = []string{"-"}
	sp.delay = time.Millisecond

	sp.Start()
	time.Sleep(3 * time.Millisecond)
	sp.Stop()

	out := buf.String()
	if !strings.Contains(out, "\n") {
		t.Fatalf("expected spinner to add spacer newline, got %q", out)
	}
	if !strings.Contains(out, "working") {
		t.Fatalf("expected spinner label in output, got %q", out)
	}
	if sp.spacerAdded {
		t.Fatalf("expected spacer flag to reset on Stop")
	}
}

func TestSpinnerHiddenViaEnv(t *testing.T) {
	master, slave := openPTYOrSkip(t)
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Fatalf("close master pty: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Fatalf("close slave pty: %v", err)
		}
	})
	t.Setenv("DOCKFORM_SPINNER_HIDDEN", "")
	visible := NewSpinner(slave, "visible")
	if !visible.enabled {
		t.Fatalf("expected spinner to be enabled on tty")
	}
	t.Setenv("DOCKFORM_SPINNER_HIDDEN", "1")
	hidden := NewSpinner(slave, "hidden")
	if hidden.enabled {
		t.Fatalf("expected spinner to be disabled when env requests hiding")
	}
}
