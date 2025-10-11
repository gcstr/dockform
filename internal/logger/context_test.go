package logger

import (
	"context"
	"io"
	"testing"
)

func TestWithContextAndFromContext(t *testing.T) {
	l, closer, err := New(Options{Out: io.Discard, Format: "json"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	if closer != nil {
		t.Cleanup(func() { _ = closer.Close() })
	}
	ctx := WithContext(context.Background(), l)
	got := FromContext(ctx)
	if got == nil {
		t.Fatal("expected logger from context")
	}
	if got != l {
		t.Fatalf("expected stored logger to be returned")
	}
	if nop := FromContext(context.Background()); nop == nil {
		t.Fatalf("expected Nop logger when context has no logger")
	}
}

func TestNewRunIDFormat(t *testing.T) {
	id := NewRunID()
	if len(id) != 12 {
		t.Fatalf("expected 12-character id, got %q", id)
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			t.Fatalf("expected hex characters, got %q", id)
		}
	}
}
