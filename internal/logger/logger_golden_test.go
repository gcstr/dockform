package logger

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// sanitizeMap removes dynamic fields before comparison.
func sanitizeMap(m map[string]any) map[string]any {
	delete(m, "time")
	delete(m, "ts")
	delete(m, "duration_ms")
	return m
}

func decodeLines(b []byte) ([]map[string]any, error) {
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	out := make([]map[string]any, 0, len(lines))
	for _, ln := range lines {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal(ln, &m); err != nil {
			return nil, err
		}
		out = append(out, sanitizeMap(m))
	}
	return out, nil
}

func TestGoldenStepJSON(t *testing.T) {
	var buf bytes.Buffer
	rt := false
	l, closer, err := New(Options{Out: &buf, Format: "json", Level: "info", ReportTimestamp: &rt})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if closer != nil {
		_ = closer.Close()
	}
	l = l.With("run_id", "abc123", "command", "dockform plan")
	l = l.With("component", "network")

	st := StartStep(l, "network_ensure", "df_net")
	st.OK(true)

	gotObjs, err := decodeLines(buf.Bytes())
	if err != nil {
		t.Fatalf("decode got: %v", err)
	}
	if len(gotObjs) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(gotObjs))
	}

	golden := filepath.Join("testdata", "step_golden.jsonl")
	wantRaw, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	wantObjs, err := decodeLines(wantRaw)
	if err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if !reflect.DeepEqual(wantObjs, gotObjs) {
		t.Fatalf("golden mismatch\nwant:%v\n----\ngot:%v", wantObjs, gotObjs)
	}
}
