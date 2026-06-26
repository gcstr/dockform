package sshmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectedOptions(t *testing.T) {
	opts := InjectedOptions("/tmp/dfssh-abc")
	joined := strings.Join(opts, " ")
	for _, want := range []string{
		"ControlMaster=auto",
		"ControlPath=/tmp/dfssh-abc/%C",
		"ControlPersist=60",
		"ServerAliveInterval=15",
		"ServerAliveCountMax=3",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
}

func TestShimArgs_OrdersOptionsBeforeIncoming(t *testing.T) {
	got := ShimArgs("/usr/bin/ssh", "/tmp/dfssh-abc", []string{"-l", "user", "host", "--", "docker"})
	if got[0] != "/usr/bin/ssh" {
		t.Fatalf("argv[0] = %q, want real ssh", got[0])
	}
	iCtrl := indexOf(got, "ControlMaster=auto")
	iHost := indexOf(got, "host")
	if iCtrl < 0 || iHost < 0 || iCtrl > iHost {
		t.Fatalf("expected injected options before incoming args: %v", got)
	}
}

func TestFindRealSSH_SkipsShimDir(t *testing.T) {
	shimDir := t.TempDir()
	realDir := t.TempDir()
	mustWriteExec(t, filepath.Join(shimDir, "ssh"))
	realSSH := filepath.Join(realDir, "ssh")
	mustWriteExec(t, realSSH)

	pathEnv := shimDir + string(os.PathListSeparator) + realDir
	got, err := FindRealSSH(shimDir, pathEnv)
	if err != nil {
		t.Fatalf("FindRealSSH: %v", err)
	}
	if got != realSSH {
		t.Fatalf("FindRealSSH = %q, want %q (must skip shim dir)", got, realSSH)
	}
}

func TestShouldRunAsShim(t *testing.T) {
	if !ShouldRunAsShim("/tmp/dfssh-x/ssh") {
		t.Fatal("expected true when invoked as ssh")
	}
	if ShouldRunAsShim("/usr/local/bin/dockform") {
		t.Fatal("expected false for normal invocation")
	}
}

func indexOf(s []string, sub string) int {
	for i, v := range s {
		if strings.Contains(v, sub) {
			return i
		}
	}
	return -1
}

func mustWriteExec(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write exec: %v", err)
	}
}
