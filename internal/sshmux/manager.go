package sshmux

import (
	"os"
	"path/filepath"
)

// Manager holds run-scoped multiplexing state so it can be torn down.
type Manager struct {
	dir      string
	oldPath  string
	prevCtrl string
	hadCtrl  bool
	active   bool
}

// Setup installs the ssh shim for the lifetime of a run. It creates a short
// run-scoped dir under /tmp (NOT $TMPDIR — macOS's is too long for the ~104-char
// ControlPath limit), symlinks `ssh` to the running dockform binary inside it,
// prepends that dir to PATH, and exports the control dir for the shim.
func Setup(selfExe string) (*Manager, error) {
	dir, err := os.MkdirTemp("/tmp", "dfssh-")
	if err != nil {
		return nil, err
	}
	if err := os.Symlink(selfExe, filepath.Join(dir, "ssh")); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	oldPath := os.Getenv("PATH")
	prevCtrl, hadCtrl := os.LookupEnv(ControlEnvVar)
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	_ = os.Setenv(ControlEnvVar, dir)
	return &Manager{dir: dir, oldPath: oldPath, prevCtrl: prevCtrl, hadCtrl: hadCtrl, active: true}, nil
}

// Dir returns the run-scoped control directory.
func (m *Manager) Dir() string {
	if m == nil {
		return ""
	}
	return m.dir
}

// Teardown restores PATH and the control env var and removes the run dir. Any
// lingering ssh master self-exits via ControlPersist. Idempotent and nil-safe.
func (m *Manager) Teardown() {
	if m == nil || !m.active {
		return
	}
	m.active = false
	_ = os.Setenv("PATH", m.oldPath)
	if m.hadCtrl {
		_ = os.Setenv(ControlEnvVar, m.prevCtrl)
	} else {
		_ = os.Unsetenv(ControlEnvVar)
	}
	_ = os.RemoveAll(m.dir)
}
