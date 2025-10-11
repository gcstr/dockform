package ui

import (
	"bytes"
	"strings"
	"testing"
)

func newTestProgress(buf *bytes.Buffer) *Progress {
	p := NewProgress(buf, "Applying")
	p.enabled = true
	p.out = buf
	return p
}

func TestProgressStartIncrementAndStop(t *testing.T) {
	var buf bytes.Buffer
	p := newTestProgress(&buf)
	p.Start(3)
	p.SetAction("sync")
	p.Increment()
	p.AdjustTotal(-1)
	p.Increment()
	p.Stop()

	out := buf.String()
	if !strings.Contains(out, "1/3") {
		t.Fatalf("expected rendered fraction, got %q", out)
	}
	if !strings.Contains(out, "sync") {
		t.Fatalf("expected action text in output, got %q", out)
	}
	if !strings.Contains(out, "\x1b[2K") {
		t.Fatalf("expected stop to clear line, got %q", out)
	}
}

func TestProgressRenderSuppressedByEnv(t *testing.T) {
	var buf bytes.Buffer
	p := newTestProgress(&buf)
	buf.Reset()
	t.Setenv("DOCKFORM_TUI_ACTIVE", "1")
	p.Start(2)
	p.SetAction("blocked")
	if buf.Len() != 0 {
		t.Fatalf("expected no output when TUI active, got %q", buf.String())
	}
}
