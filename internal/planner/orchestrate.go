package planner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
)

// DaemonResult holds the outcome of executing an operation on one daemon.
type DaemonResult struct {
	DaemonName string
	Err        error
}

// ExecuteAcrossDaemons runs fn for each daemon, either in parallel or sequentially
// based on the planner's parallel flag.
func (p *Planner) ExecuteAcrossDaemons(ctx context.Context, cfg *manifest.Config, fn func(ctx context.Context, daemonName string) error) error {
	daemonNames := sortedKeys(cfg.Daemons)
	if len(daemonNames) == 0 {
		return nil
	}

	if !p.parallel || len(daemonNames) == 1 {
		return executeSequential(ctx, daemonNames, fn)
	}
	return executeParallel(ctx, daemonNames, fn)
}

func executeSequential(ctx context.Context, daemonNames []string, fn func(ctx context.Context, daemonName string) error) error {
	for _, name := range daemonNames {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := fn(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

func executeParallel(ctx context.Context, daemonNames []string, fn func(ctx context.Context, daemonName string) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	var errs []DaemonResult
	var wg sync.WaitGroup

	for _, name := range daemonNames {
		wg.Add(1)
		go func(daemonName string) {
			defer wg.Done()
			if err := fn(ctx, daemonName); err != nil {
				mu.Lock()
				errs = append(errs, DaemonResult{DaemonName: daemonName, Err: err})
				mu.Unlock()
				cancel() // signal other goroutines to stop
			}
		}(name)
	}

	wg.Wait()

	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0].Err
	}

	// Sort errors by daemon name for deterministic output
	sort.Slice(errs, func(i, j int) bool { return errs[i].DaemonName < errs[j].DaemonName })
	var msgs []string
	for _, r := range errs {
		msgs = append(msgs, fmt.Sprintf("daemon %s: %v", r.DaemonName, r.Err))
	}
	return apperr.New("planner.ExecuteAcrossDaemons", apperr.External, "multiple daemon errors:\n  %s", strings.Join(msgs, "\n  "))
}
