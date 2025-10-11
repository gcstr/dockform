package ui

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync"
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
		t.Fatalf("pty open: %v", err)
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

func TestUILogWriterBuffersLines(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	writer := &UILogWriter{send: func(s string) {
		mu.Lock()
		lines = append(lines, s)
		mu.Unlock()
	}}

	if _, err := writer.Write([]byte("hello\npartial")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("expected buffered line, got %v", lines)
	}
	if _, err := writer.Write([]byte(" line\n")); err != nil {
		t.Fatalf("write continuation: %v", err)
	}
	if len(lines) != 2 || lines[1] != "partial line" {
		t.Fatalf("expected second line, got %v", lines)
	}
	if writer.Fd() != os.Stdout.Fd() {
		t.Fatalf("expected UILogWriter to report stdout FD")
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

	if updated, _ := m.Update(appendLog{line: "first"}); updated != nil {
		m = updated.(model)
	}
	if updated, _ := m.Update(appendLog{line: "second"}); updated != nil {
		m = updated.(model)
	}
	if updated, _ := m.Update(done{report: "done"}); updated != nil {
		m = updated.(model)
	}
	if m.state != stateFinal || m.finalReport != "done" {
		t.Fatalf("expected final state with report, got %+v", m)
	}
	view := m.View()
	if !bytes.Contains([]byte(view), []byte("done")) {
		t.Fatalf("expected final report in view, got %q", view)
	}

	m = model{state: stateRunning, width: 6}
	for i := 0; i < 7; i++ {
		if updated, _ := m.Update(appendLog{line: "line"}); updated != nil {
			m = updated.(model)
		}
	}
	if len(m.logLines) != 5 {
		t.Fatalf("expected logs capped at 5, got %d", len(m.logLines))
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
	m := model{state: stateRunning, width: 20, logLines: []string{"line"}}
	view := m.View()
	if !strings.Contains(view, "line") {
		t.Fatalf("expected running view to include log line")
	}
}
