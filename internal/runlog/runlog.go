// Package runlog owns the always-on apply run log: where it lives, how it is
// opened, and how old ones are pruned. Logging must never fail an apply, so
// every fallible step degrades to a warning instead of an error.
package runlog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gcstr/dockform/internal/logger"
)

// keepRuns is how many run logs survive a prune.
const keepRuns = 10

// runLogPrefix and runLogSuffix bound the filename shape Open generates
// (e.g. "apply-20060102-150405.log"). prune only ever counts or deletes
// entries matching this shape, so it never touches files it does not own.
const (
	runLogPrefix = "apply-"
	runLogSuffix = ".log"
)

// Handle is an open run log.
type Handle struct {
	path    string
	log     logger.Logger
	closer  io.Closer
	warning string
}

func (h *Handle) Path() string          { return h.path }
func (h *Handle) Logger() logger.Logger { return h.log }

// Warning is a non-empty message when the log did not land where intended.
func (h *Handle) Warning() string { return h.warning }

func (h *Handle) Close() error {
	if h == nil || h.closer == nil {
		return nil
	}
	return h.closer.Close()
}

// Open creates a run log under <manifestDir>/.dockform/logs, falling back to the
// user state directory when that is not writable. It never returns an error for
// a location problem — only for a failure to open any log at all.
func Open(manifestDir string) (*Handle, error) {
	name := fmt.Sprintf("%s%s%s", runLogPrefix, time.Now().UTC().Format("20060102-150405"), runLogSuffix)

	primary := filepath.Join(manifestDir, ".dockform", "logs")
	if h, err := openIn(primary, name); err == nil {
		_ = prune(primary, keepRuns)
		return h, nil
	}

	fallbackBase, err := os.UserConfigDir()
	if err != nil || fallbackBase == "" {
		fallbackBase = os.TempDir()
	}
	fallback := filepath.Join(fallbackBase, "dockform", "logs")
	h, err := openIn(fallback, name)
	if err != nil {
		return nil, err
	}
	h.warning = fmt.Sprintf("could not write to %s; logging to %s instead", primary, h.path)
	_ = prune(fallback, keepRuns)
	return h, nil
}

func openIn(dir, name string) (*Handle, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)

	// The run log is always complete: debug regardless of --log-level, because
	// the records that matter after a failure (ssh_retry and friends) are debug.
	l, closer, err := logger.New(logger.Options{
		Out:       io.Discard,
		Level:     "debug",
		FileLevel: "debug",
		Format:    "json",
		LogFile:   path,
	})
	if err != nil {
		return nil, err
	}
	return &Handle{path: path, log: l, closer: closer}, nil
}

// prune deletes all but the newest keep run logs. Names embed a sortable UTC
// timestamp, so lexicographic order is chronological. Only entries matching
// the shape Open generates (runLogPrefix + ... + runLogSuffix) are counted
// or deleted; anything else in the directory is left untouched, whether it
// sorts before the keep window (and would otherwise be deleted) or after it
// (and would otherwise occupy a keep slot).
func prune(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, runLogPrefix) || !strings.HasSuffix(name, runLogSuffix) {
			continue
		}
		names = append(names, name)
	}
	if len(names) <= keep {
		return nil
	}
	sort.Strings(names)
	for _, name := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}
