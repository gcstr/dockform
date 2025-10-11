package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSelectModelNavigation(t *testing.T) {
	m := selectModel{title: "Choose", options: []string{"a", "b", "c"}}
	if updated, _ := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'j'}})); updated != nil {
		m = updated.(selectModel)
	}
	if m.cursor != 1 {
		t.Fatalf("expected cursor to move down, got %d", m.cursor)
	}
	if updated, _ := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'k'}})); updated != nil {
		m = updated.(selectModel)
	}
	if m.cursor != 0 {
		t.Fatalf("expected cursor to move up, got %d", m.cursor)
	}
	if updated, _ := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEsc})); updated != nil {
		m = updated.(selectModel)
	}
	if m.confirmed {
		t.Fatalf("expected escape to cancel selection")
	}
	if updated, _ := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter})); updated != nil {
		m = updated.(selectModel)
	}
	if !m.confirmed || m.choice != 0 {
		t.Fatalf("expected selection confirmation, got confirmed=%v choice=%d", m.confirmed, m.choice)
	}

	view := m.View()
	if !strings.Contains(view, "[x] a") {
		t.Fatalf("expected highlighted option in view, got: %q", view)
	}
}

func TestReadLineNonTTY(t *testing.T) {
	var in bytes.Buffer
	in.WriteString("hello world\n")
	out, err := ReadLineNonTTY(&in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello world" {
		t.Fatalf("expected trimmed line, got %q", out)
	}
}

func TestSelectOneTTYNonTTYFallback(t *testing.T) {
	var in bytes.Buffer
	var out bytes.Buffer
	idx, ok, err := SelectOneTTY(&in, &out, "title", []string{"a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok || idx != -1 {
		t.Fatalf("expected non-tty call to skip selection")
	}
}

func TestSelectOneTTYWithTTY(t *testing.T) {
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
	discardPTY(master)
	resultCh := make(chan struct {
		idx int
		ok  bool
		err error
	}, 1)
	go func() {
		idx, ok, err := SelectOneTTY(slave, slave, "Title", []string{"a", "b"})
		resultCh <- struct {
			idx int
			ok  bool
			err error
		}{idx, ok, err}
	}()
	time.Sleep(50 * time.Millisecond)
	// Select second option: send 'j' then enter
	_, _ = master.Write([]byte("j"))
	_, _ = master.Write([]byte{''})
	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("select tty error: %v", res.err)
		}
		if !res.ok || res.idx != 1 {
			t.Fatalf("expected to select index 1, got ok=%v idx=%d", res.ok, res.idx)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for selection")
	}
}
