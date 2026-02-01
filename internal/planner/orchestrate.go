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

// ContextResult holds the outcome of executing an operation on one context.
type ContextResult struct {
	ContextName string
	Err         error
}

// ExecuteAcrossContexts runs fn for each context, either in parallel or sequentially
// based on the planner's parallel flag.
func (p *Planner) ExecuteAcrossContexts(ctx context.Context, cfg *manifest.Config, fn func(ctx context.Context, contextName string) error) error {
	contextNames := sortedKeys(cfg.Contexts)
	if len(contextNames) == 0 {
		return nil
	}

	if !p.parallel || len(contextNames) == 1 {
		return executeSequential(ctx, contextNames, fn)
	}
	return executeParallel(ctx, contextNames, fn)
}

func executeSequential(ctx context.Context, contextNames []string, fn func(ctx context.Context, contextName string) error) error {
	for _, name := range contextNames {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := fn(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

func executeParallel(ctx context.Context, contextNames []string, fn func(ctx context.Context, contextName string) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	var errs []ContextResult
	var wg sync.WaitGroup

	for _, name := range contextNames {
		wg.Add(1)
		go func(contextName string) {
			defer wg.Done()
			if err := fn(ctx, contextName); err != nil {
				mu.Lock()
				errs = append(errs, ContextResult{ContextName: contextName, Err: err})
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

	// Sort errors by context name for deterministic output
	sort.Slice(errs, func(i, j int) bool { return errs[i].ContextName < errs[j].ContextName })
	var msgs []string
	for _, r := range errs {
		msgs = append(msgs, fmt.Sprintf("context %s: %v", r.ContextName, r.Err))
	}
	return apperr.New("planner.ExecuteAcrossContexts", apperr.External, "multiple context errors:\n  %s", strings.Join(msgs, "\n  "))
}
