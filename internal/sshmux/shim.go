// Package sshmux provides run-scoped SSH connection multiplexing. The dockform
// binary, when invoked through a `ssh` symlink on PATH, acts as a transparent
// shim that execs the real ssh with ControlMaster options injected, so docker's
// ssh connection helper reuses one connection per host for a run's lifetime.
package sshmux

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ControlEnvVar carries the run-scoped ControlPath base dir to the shim process.
// Its presence also signals that multiplexing is active for this run.
const ControlEnvVar = "DOCKFORM_SSH_CONTROL_DIR"

// InjectedOptions returns the ssh -o flags that enable run-scoped multiplexing.
// ControlPath uses ssh's %C token (a hash of host/port/user) to stay short and
// unique per destination, well under the ~104-char unix socket limit.
func InjectedOptions(controlDir string) []string {
	return []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + filepath.Join(controlDir, "%C"),
		"-o", "ControlPersist=60",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
	}
}

// FindRealSSH returns the first `ssh` on pathEnv that is not inside shimDir, so
// the shim never recurses into itself.
func FindRealSSH(shimDir, pathEnv string) (string, error) {
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" || sameDir(dir, shimDir) {
			continue
		}
		cand := filepath.Join(dir, "ssh")
		if isExecutable(cand) {
			return cand, nil
		}
	}
	return "", fmt.Errorf("sshmux: real ssh not found on PATH (excluding %s)", shimDir)
}

// ShimArgs builds argv for exec'ing the real ssh: realSSH, injected options,
// then the original incoming args (the destination and docker's own flags).
func ShimArgs(realSSH, controlDir string, incoming []string) []string {
	argv := make([]string, 0, 1+10+len(incoming))
	argv = append(argv, realSSH)
	argv = append(argv, InjectedOptions(controlDir)...)
	argv = append(argv, incoming...)
	return argv
}

// ShouldRunAsShim reports whether the binary was invoked through its ssh symlink.
func ShouldRunAsShim(arg0 string) bool {
	return filepath.Base(arg0) == "ssh"
}

// Run is the shim entrypoint. It execs the real ssh, injecting multiplexing
// options when a control dir is configured. It does not return on success.
func Run(arg0 string, incoming []string) error {
	shimDir := filepath.Dir(arg0)
	realSSH, err := FindRealSSH(shimDir, os.Getenv("PATH"))
	if err != nil {
		return err
	}
	var argv []string
	if controlDir := os.Getenv(ControlEnvVar); controlDir != "" {
		argv = ShimArgs(realSSH, controlDir, incoming)
	} else {
		argv = append([]string{realSSH}, incoming...)
	}
	return syscall.Exec(realSSH, argv, os.Environ())
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.Abs(a)
	rb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return ra == rb
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}
