package logger

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestJSONConsistency(t *testing.T) {
	var buf bytes.Buffer
	l, closer, err := New(Options{Out: &buf, Format: "json", Level: "debug"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if closer != nil {
		_ = closer.Close()
	}
	l = l.With("run_id", "abc123", "command", "dockform plan")
	l2 := l.With("component", "network")

	st := StartStep(l2, "network_ensure", "df_net")
	st.OK(true)

	// Parse the last line as JSON and verify stable keys exist
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d: %s", len(lines), buf.String())
	}
	got := map[string]any{}
	if err := json.Unmarshal(lines[len(lines)-1], &got); err != nil {
		t.Fatalf("json: %v: %s", err, string(lines[len(lines)-1]))
	}
	// Required keys
	for _, k := range []string{"run_id", "command", "component", "status", "action", "resource", "changed", "duration_ms"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("missing key %q in %v", k, got)
		}
	}
}
