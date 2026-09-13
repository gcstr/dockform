package logger

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestFileLevelIndependentOfConsoleLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.log")

	l, closer, err := New(Options{Out: io.Discard, Level: "error", Format: "json", LogFile: path, FileLevel: "debug"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.Debug("ssh_retry", "attempt", 2)
	if closer != nil {
		_ = closer.Close()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "ssh_retry") {
		t.Fatalf("debug record missing from file sink while console level was error:\n%s", data)
	}
}

func TestFileLevelDefaultsToConsoleLevel(t *testing.T) {
	t.Run("debug console level lets a debug record reach the file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "run.log")

		l, closer, err := New(Options{Out: io.Discard, Level: "debug", Format: "json", LogFile: path})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		l.Debug("should_appear")
		if closer != nil {
			_ = closer.Close()
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read log: %v", err)
		}
		if !strings.Contains(string(data), "should_appear") {
			t.Fatalf("FileLevel did not fall back to the debug console level:\n%s", data)
		}
	})

	t.Run("error console level keeps a debug record out of the file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "run.log")

		l, closer, err := New(Options{Out: io.Discard, Level: "error", Format: "json", LogFile: path})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		l.Debug("should_not_appear")
		if closer != nil {
			_ = closer.Close()
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read log: %v", err)
		}
		if strings.Contains(string(data), "should_not_appear") {
			t.Fatal("FileLevel default changed existing --log-file behaviour")
		}
	})
}
