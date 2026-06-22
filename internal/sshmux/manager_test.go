package sshmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupAndTeardown(t *testing.T) {
	selfExe := filepath.Join(t.TempDir(), "dockform")
	if err := os.WriteFile(selfExe, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write selfExe: %v", err)
	}

	oldPath := os.Getenv("PATH")
	if _, had := os.LookupEnv(ControlEnvVar); had {
		t.Skip("ControlEnvVar already set in environment")
	}

	mgr, err := Setup(selfExe)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if !strings.HasPrefix(mgr.Dir(), "/tmp/dfssh-") {
		t.Fatalf("dir %q not under /tmp/dfssh-", mgr.Dir())
	}
	if len(mgr.Dir()) > 40 {
		t.Fatalf("control dir too long for ControlPath budget: %q (%d)", mgr.Dir(), len(mgr.Dir()))
	}
	link := filepath.Join(mgr.Dir(), "ssh")
	if target, err := os.Readlink(link); err != nil || target != selfExe {
		t.Fatalf("symlink target = %q (err %v), want %q", target, err, selfExe)
	}
	if !strings.HasPrefix(os.Getenv("PATH"), mgr.Dir()+string(os.PathListSeparator)) {
		t.Fatalf("PATH not prepended with shim dir: %q", os.Getenv("PATH"))
	}
	if os.Getenv(ControlEnvVar) != mgr.Dir() {
		t.Fatalf("%s = %q, want %q", ControlEnvVar, os.Getenv(ControlEnvVar), mgr.Dir())
	}

	mgr.Teardown()

	if os.Getenv("PATH") != oldPath {
		t.Fatalf("PATH not restored: %q != %q", os.Getenv("PATH"), oldPath)
	}
	if _, ok := os.LookupEnv(ControlEnvVar); ok {
		t.Fatalf("%s not unset after teardown", ControlEnvVar)
	}
	if _, err := os.Stat(mgr.Dir()); !os.IsNotExist(err) {
		t.Fatalf("run dir not removed: %v", err)
	}

	// Idempotent / nil-safe.
	mgr.Teardown()
	var nilMgr *Manager
	nilMgr.Teardown()
}
