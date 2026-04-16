package ui

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"

	"github.com/gcstr/dockform/internal/logger"
)

func TestRunWithRollingLogNonTTYFallback(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
		if err := r.Close(); err != nil {
			t.Errorf("close read pipe: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Errorf("close write pipe: %v", err)
		}
	})

	called := false
	report, err := RunWithRollingLog(context.Background(), func(ctx context.Context) (string, error) {
		called = true
		return "final report", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatalf("expected callback to be invoked")
	}
	if report != "final report" {
		t.Fatalf("unexpected report: %q", report)
	}
}

func TestRunWithRollingLogTTY(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("unable to open pty: %v", err)
	}
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Errorf("close master pty: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Errorf("close slave pty: %v", err)
		}
	})
	oldStdout := os.Stdout
	os.Stdout = slave
	defer func() { os.Stdout = oldStdout }()

	baseLogger, closer, err := logger.New(logger.Options{Out: io.Discard, Level: "info", Format: "json"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	if closer != nil {
		t.Cleanup(func() {
			if err := closer.Close(); err != nil {
				t.Errorf("close logger: %v", err)
			}
		})
	}

	ctx := logger.WithContext(context.Background(), baseLogger)
	report, err := RunWithRollingLog(ctx, func(ctx context.Context) (string, error) {
		logger.FromContext(ctx).Info("running", "msg", "hello")
		return "done", nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "could not open a new TTY") {
			t.Skip("TTY not available in test environment")
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if report != "done" {
		t.Fatalf("expected report, got %q", report)
	}
}

func TestDisplayLoggerEmit(t *testing.T) {
	var lines []string
	dl := newDisplayLogger(func(line string) { lines = append(lines, line) })

	// Attach persistent noise fields (as root.go does) and a component field.
	child := dl.With("run_id", "abc123", "command", "dockform plan").With("component", "dockercli")

	// Simulate logger.StartStep output.
	child.Info("docker_exec", "status", "started", "action", "docker_exec", "resource", "mynet", "resource_kind", "process")

	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	got := StripANSI(lines[0])

	// run_id and command must be absent.
	if strings.Contains(got, "run_id") || strings.Contains(got, "command") {
		t.Fatalf("noise fields should be stripped, got: %q", got)
	}
	// action+status collapsed.
	if !strings.Contains(got, "docker_exec(started)") {
		t.Fatalf("expected collapsed action(status), got: %q", got)
	}
	// component and resource_kind kept.
	if !strings.Contains(got, "component=dockercli") {
		t.Fatalf("expected component field, got: %q", got)
	}
	if !strings.Contains(got, "resource_kind=process") {
		t.Fatalf("expected resource_kind field, got: %q", got)
	}
	// Time (HH:MM:SS) and level present.
	if !strings.Contains(got, "INFO") {
		t.Fatalf("expected level in output, got: %q", got)
	}
}

func TestDisplayLoggerDebugSuppressed(t *testing.T) {
	var lines []string
	dl := newDisplayLogger(func(line string) { lines = append(lines, line) })
	dl.Debug("should not appear")
	if len(lines) != 0 {
		t.Fatalf("debug should be suppressed, got: %v", lines)
	}
}

func TestDisplayLoggerWithInheritance(t *testing.T) {
	var lines []string
	base := newDisplayLogger(func(line string) { lines = append(lines, line) })
	child := base.With("run_id", "x").With("component", "net")

	child.Info("network_ensure", "status", "ok", "action", "network_ensure")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	got := StripANSI(lines[0])
	if strings.Contains(got, "run_id") {
		t.Fatalf("run_id should be stripped, got: %q", got)
	}
	if !strings.Contains(got, "network_ensure(ok)") {
		t.Fatalf("expected collapsed action(status), got: %q", got)
	}
	if !strings.Contains(got, "component=net") {
		t.Fatalf("expected component field from With(), got: %q", got)
	}
}

func TestModelUpdateAndView(t *testing.T) {
	ch := make(chan struct{}, 1)
	m := model{state: stateRunning, width: 10, cancelCh: ch}

	if updated, _ := m.Update(tea.WindowSizeMsg{Width: 40}); updated != nil {
		m = updated.(model)
	}
	if m.width != 40 {
		t.Fatalf("expected width update, got %d", m.width)
	}

	// Running view is a minimal spacer — log lines stream via p.Println, not the view.
	view := m.View()
	if view != "\n" {
		t.Fatalf("expected running view to be a single newline, got %q", view)
	}

	if updated, _ := m.Update(done{report: "done"}); updated != nil {
		m = updated.(model)
	}
	if m.state != stateFinal || m.finalReport != "done" {
		t.Fatalf("expected final state with report, got %+v", m)
	}
	view = m.View()
	if !bytes.Contains([]byte(view), []byte("done")) {
		t.Fatalf("expected final report in view, got %q", view)
	}
}

func TestModelCtrlCTriggersCancel(t *testing.T) {
	ch := make(chan struct{}, 1)
	m := model{state: stateRunning, cancelCh: ch}
	if updated, _ := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyCtrlC})); updated != nil {
		updatedModel, ok := updated.(model)
		if !ok {
			t.Fatalf("expected model type from update, got %T", updated)
		}
		if updatedModel.cancelCh != ch {
			t.Fatalf("expected cancel channel to remain unchanged")
		}
	}
	select {
	case <-ch:
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("expected cancel signal on ctrl+c")
	}
}


func TestTruncOneRowANSI(t *testing.T) {
	if truncOneRowANSI("abcdef", 2) != "" {
		t.Fatalf("expected empty when width below border size")
	}
	if truncOneRowANSI("abc", 10) != "abc" {
		t.Fatalf("expected value unchanged when width sufficient")
	}
	if trunc := truncOneRowANSI("abcdef", 6); trunc != "abcd" {
		t.Fatalf("expected truncation to preserve prefix, got %q", trunc)
	}
}

func TestModelInitAndRunningView(t *testing.T) {
	if cmd := (model{}).Init(); cmd != nil {
		t.Fatalf("expected nil init cmd")
	}
	// Running view is a minimal spacer — content arrives via p.Println.
	m := model{state: stateRunning, width: 20}
	view := m.View()
	if view != "\n" {
		t.Fatalf("expected running view to be a single newline, got %q", view)
	}
}
