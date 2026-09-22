package dockercli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func stubDocker(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

type lineLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *lineLog) add(line []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, string(line))
}

func (l *lineLog) count(substr string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, s := range l.lines {
		if strings.Contains(s, substr) {
			n++
		}
	}
	return n
}

func TestRunDetailed_StderrLineReceivesLinesAndStderrStaysBuffered(t *testing.T) {
	stubDocker(t, `printf '%s\n' '{"id":"Container a-web-1","status":"Working","text":"Creating"}' 1>&2
printf '%s\n' '{"id":"Container a-web-1","status":"Done","text":"Created"}' 1>&2
`)
	log := &lineLog{}
	res, err := SystemExec{}.RunDetailed(context.Background(), Options{StderrLine: log.add}, "compose", "up")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := log.count(`"Container a-web-1"`); got != 2 {
		t.Fatalf("callback got %d event lines, want 2: %q", got, log.lines)
	}
	if !strings.Contains(res.Stderr, `"text":"Created"`) {
		t.Fatalf("stderr must still be buffered into Result.Stderr, got %q", res.Stderr)
	}
}

// Streaming stdout disables SSH retries; a stderr line callback must not.
// Every attempt's lines are delivered, so the consumer sees a replay.
func TestRunDetailed_StderrLineKeepsSSHRetry(t *testing.T) {
	dir := stubDocker(t, "")
	counter := filepath.Join(dir, "attempts")
	script := `n=$(cat "` + counter + `" 2>/dev/null || echo 0); n=$((n+1)); echo $n > "` + counter + `"
printf '%s\n' '{"id":"Container a-web-1","status":"Working","text":"Creating"}' 1>&2
if [ "$n" = "1" ]; then echo 'Connection closed by 10.0.0.1 port 22' 1>&2; exit 1; fi
printf '%s\n' '{"id":"Container a-web-1","status":"Done","text":"Started"}' 1>&2
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	orig := sshRetryBaseDelay
	sshRetryBaseDelay = time.Millisecond
	t.Cleanup(func() { sshRetryBaseDelay = orig })

	log := &lineLog{}
	s := SystemExec{sem: make(chan struct{}, 1)}
	if _, err := s.RunDetailed(context.Background(), Options{StderrLine: log.add}, "compose", "up"); err != nil {
		t.Fatalf("expected the retry to succeed, got: %v", err)
	}
	attempts, _ := os.ReadFile(counter)
	if strings.TrimSpace(string(attempts)) != "2" {
		t.Fatalf("attempts = %q, want 2", attempts)
	}
	if got := log.count(`"text":"Creating"`); got != 2 {
		t.Fatalf("got %d Creating lines, want 2 (one per attempt)", got)
	}
	if got := log.count(`"text":"Started"`); got != 1 {
		t.Fatalf("got %d Started lines, want 1", got)
	}
}
