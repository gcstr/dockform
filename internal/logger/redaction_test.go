package logger

import (
	"bytes"
	"errors"
	"testing"
)

func TestRedactPairsMasksSensitiveKeys(t *testing.T) {
	redacted := redactPairs([]any{"password", "secret", "note", "token=abc"})
	if redacted[1] != "[REDACTED]" {
		t.Fatalf("expected password value to be redacted, got %v", redacted[1])
	}
	if v := redacted[3].(string); !bytes.Contains([]byte(v), []byte("token=[REDACTED]")) {
		t.Fatalf("expected inline token to be redacted, got %q", v)
	}
}

func TestRedactErrorMasksTokens(t *testing.T) {
	err := errors.New("request failed: token=abcd1234")
	if got := redactError(err); got != "request failed: token=[REDACTED]" {
		t.Fatalf("expected error text to be redacted, got %q", got)
	}
}

func TestStepFailIncludesRedactedError(t *testing.T) {
	var buf bytes.Buffer
	l, closer, err := New(Options{Out: &buf, Format: "json", Level: "info"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	if closer != nil {
		t.Cleanup(func() { _ = closer.Close() })
	}
	step := StartStep(l, "apply", "service", "token", "secret")
	_ = step.Fail(errors.New("token=abcd"))
	line := bytes.TrimSpace(buf.Bytes())
	if !bytes.Contains(line, []byte("token=[REDACTED]")) {
		t.Fatalf("expected log output to be redacted, got: %s", line)
	}
}
