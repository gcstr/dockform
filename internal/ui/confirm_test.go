package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmYesTTY_AcceptsYes(t *testing.T) {
	in := bytes.NewBufferString("yes\n")
	var out bytes.Buffer
	ok, entered, err := ConfirmYesTTY(in, &out)
	if err != nil {
		t.Fatalf("confirm prompt error: %v", err)
	}
	if !ok || entered != "yes" {
		t.Fatalf("expected ok=true and entered=\"yes\", got ok=%v entered=%q", ok, entered)
	}
}

func TestConfirmYesTTY_RejectsNonYes(t *testing.T) {
	in := bytes.NewBufferString("no\n")
	var out bytes.Buffer
	ok, entered, err := ConfirmYesTTY(in, &out)
	if err != nil {
		t.Fatalf("confirm prompt error: %v", err)
	}
	if ok || entered != "no" {
		t.Fatalf("expected ok=false and entered=\"no\", got ok=%v entered=%q", ok, entered)
	}
}

func TestConfirmIdentifierTTY(t *testing.T) {
	in := bytes.NewBufferString("my-stack\n")
	var out bytes.Buffer
	ok, entered, err := ConfirmIdentifierTTY(in, &out, "my-stack")
	if err != nil {
		t.Fatalf("confirm identifier error: %v", err)
	}
	if !ok || entered != "my-stack" {
		t.Fatalf("expected confirmation to succeed, got ok=%v entered=%q", ok, entered)
	}
}

func TestConfirmModelHandlesInput(t *testing.T) {
	cm := newConfirmModel()
	cm.ti.SetValue("yes")
	if updated, _ := cm.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter})); updated != nil {
		cm = updated.(confirmModel)
	}
	if !cm.confirmed || cm.value != "yes" {
		t.Fatalf("expected confirmation to be recorded, got %+v", cm)
	}
	cm = newConfirmModel()
	if updated, _ := cm.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEsc})); updated != nil {
		cm = updated.(confirmModel)
	}
	if cm.confirmed {
		t.Fatalf("expected escape to cancel confirmation")
	}
}

func TestConfirmIdentifierModelHandlesInput(t *testing.T) {
	im := newConfirmIdentifierModel("demo")
	im.ti.SetValue("demo")
	if updated, _ := im.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter})); updated != nil {
		im = updated.(confirmIdentifierModel)
	}
	if !im.confirmed {
		t.Fatalf("expected identifier confirmation to succeed")
	}
	view := im.View()
	if !strings.Contains(view, "demo") {
		t.Fatalf("expected identifier in view, got: %q", view)
	}
}

func TestConfirmYesTTYWithTTY(t *testing.T) {
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
		ok  bool
		val string
		err error
	}, 1)
	go func() {
		ok, val, err := ConfirmYesTTY(slave, slave)
		resultCh <- struct {
			ok  bool
			val string
			err error
		}{ok, val, err}
	}()
	time.Sleep(50 * time.Millisecond)
	_, _ = master.Write([]byte("yes\r"))
	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("confirm tty error: %v", res.err)
		}
		if !res.ok || res.val != "yes" {
			t.Fatalf("expected yes confirmation, got ok=%v val=%q", res.ok, res.val)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for confirm prompt")
	}
}

func TestConfirmIdentifierTTYWithTTY(t *testing.T) {
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
		ok  bool
		val string
		err error
	}, 1)
	go func() {
		ok, val, err := ConfirmIdentifierTTY(slave, slave, "demo")
		resultCh <- struct {
			ok  bool
			val string
			err error
		}{ok, val, err}
	}()
	time.Sleep(50 * time.Millisecond)
	_, _ = master.Write([]byte("demo\r"))
	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("confirm identifier tty error: %v", res.err)
		}
		if !res.ok || res.val != "demo" {
			t.Fatalf("expected identifier confirmation, got ok=%v val=%q", res.ok, res.val)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for identifier prompt")
	}
}
