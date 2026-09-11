package dockercli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Piping the labeled compose document must not cost compose up its SSH retries:
// StdinData is replayed on every attempt, where a plain io.Reader would be empty
// on the second one.
func TestRunDetailed_StdinDataSurvivesSSHRetry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub")
	}
	dir := t.TempDir()
	counter := filepath.Join(dir, "attempts")
	script := "#!/bin/sh\n" +
		"n=$(cat \"" + counter + "\" 2>/dev/null || echo 0); n=$((n+1)); echo $n > \"" + counter + "\"\n" +
		"if [ \"$n\" = \"1\" ]; then echo 'Connection closed by 10.0.0.1 port 22' 1>&2; exit 1; fi\n" +
		"cat\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	orig := sshRetryBaseDelay
	sshRetryBaseDelay = time.Millisecond
	t.Cleanup(func() { sshRetryBaseDelay = orig })

	s := SystemExec{sem: make(chan struct{}, 1)}
	res, err := s.RunDetailed(context.Background(), Options{StdinData: []byte("services: {}\n")}, "compose", "-f", "-", "config")
	if err != nil {
		t.Fatalf("expected the retry to succeed, got: %v", err)
	}
	if res.Stdout != "services: {}\n" {
		t.Fatalf("second attempt got stdin %q, want the full document", res.Stdout)
	}
}
