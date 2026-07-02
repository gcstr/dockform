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

// ExecutionMode controls how executeParallel reacts to a child error.
type ExecutionMode int

const (
	// FailFast cancels sibling goroutines as soon as one context errors.
	// Appropriate for read-only/discovery operations where an in-flight
	// docker command can be safely interrupted. Any sibling error caused by
	// that cancellation is classified as aborted, not as a genuine failure.
	FailFast ExecutionMode = iota
	// RunToCompletion lets every context run to completion regardless of
	// sibling errors. Required for mutating operations (apply/destroy/prune)
	// where killing an in-flight `compose up`/`compose down` on an unrelated,
	// healthy host is worse than waiting for it to finish.
	RunToCompletion
)

// ExecuteAcrossContexts runs fn for each context, either in parallel or sequentially
// based on the planner's parallel flag. Mutating operations (apply/destroy/prune)
// must run in RunToCompletion mode so a failure on one context never kills an
// in-flight mutation on another; read-only/discovery operations may use FailFast.
func (p *Planner) ExecuteAcrossContexts(ctx context.Context, cfg *manifest.Config, fn func(ctx context.Context, contextName string) error) error {
	return p.ExecuteAcrossContextsMode(ctx, cfg, RunToCompletion, fn)
}

// ExecuteAcrossContextsMode is like ExecuteAcrossContexts but lets the caller
// pick the orchestration mode explicitly.
func (p *Planner) ExecuteAcrossContextsMode(ctx context.Context, cfg *manifest.Config, mode ExecutionMode, fn func(ctx context.Context, contextName string) error) error {
	contextNames := sortedKeys(cfg.Contexts)
	if len(contextNames) == 0 {
		return nil
	}

	if !p.parallel || len(contextNames) == 1 {
		return executeSequential(ctx, contextNames, fn)
	}
	return executeParallel(ctx, contextNames, mode, fn)
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

func executeParallel(parentCtx context.Context, contextNames []string, mode ExecutionMode, fn func(ctx context.Context, contextName string) error) error {
	runCtx := parentCtx
	var cancel context.CancelFunc
	if mode == FailFast {
		runCtx, cancel = context.WithCancel(parentCtx)
		defer cancel()
	}

	var mu sync.Mutex
	var errs []ContextResult
	var wg sync.WaitGroup
	// firstFailure records which context triggered the cancellation, so that
	// exactly that one is never classified as aborted even though, by the
	// time we get around to sorting/classifying, runCtx.Err() is already set.
	firstFailure := ""
	cancelled := false

	for _, name := range contextNames {
		wg.Add(1)
		go func(contextName string) {
			defer wg.Done()
			if err := fn(runCtx, contextName); err != nil {
				mu.Lock()
				errs = append(errs, ContextResult{ContextName: contextName, Err: err})
				if mode == FailFast && !cancelled {
					cancelled = true
					firstFailure = contextName
				}
				mu.Unlock()
				if mode == FailFast && cancel != nil {
					cancel() // signal other goroutines to stop
				}
			}
		}(name)
	}

	wg.Wait()

	if len(errs) == 0 {
		return nil
	}

	// Sort errors by context name for deterministic output
	sort.Slice(errs, func(i, j int) bool { return errs[i].ContextName < errs[j].ContextName })

	if mode == FailFast {
		// Only classify children as aborted when the cancellation was OURS:
		// the parent context is still alive while the run context is
		// canceled, which can only mean our fail-fast cancel() fired in
		// response to firstFailure. If the parent itself was canceled (user
		// Ctrl-C, upstream deadline), no sibling caused anything — leave all
		// errors as-is; upstream already renders user-canceled runs.
		if parentCtx.Err() == nil && runCtx.Err() != nil {
			errs = classifyAborted(errs, firstFailure)
		}
	}

	if len(errs) == 1 {
		return errs[0].Err
	}

	// Build the summary message so aborted contexts (cut short only because a
	// sibling failed first) are never worded as failures alongside the
	// genuine one(s) — design requirement: the aggregate must clearly
	// distinguish the two and never present an aborted context as failed.
	var msgs []string
	wrapped := make([]error, 0, len(errs))
	failureCount := 0
	for _, r := range errs {
		wrapped = append(wrapped, &apperr.ContextError{ContextName: r.ContextName, Err: r.Err})
		if apperr.IsAborted(r.Err) {
			msgs = append(msgs, fmt.Sprintf("context %s: aborted: another context failed", r.ContextName))
			continue
		}
		failureCount++
		msgs = append(msgs, fmt.Sprintf("context %s: %v", r.ContextName, r.Err))
	}
	summary := fmt.Sprintf("%d context(s) failed", failureCount)
	if failureCount != len(errs) {
		summary = fmt.Sprintf("%s (%d aborted after a sibling failure)", summary, len(errs)-failureCount)
	}
	// Preserve each child's underlying cause (rather than pre-stringifying with
	// %v) so the deepest error detail, e.g. captured compose stderr, survives
	// through to printUserFriendly instead of being discarded here.
	return &apperr.E{
		Op:   "planner.ExecuteAcrossContexts",
		Kind: apperr.External,
		Err:  &apperr.MultiError{Errors: wrapped},
		Msg:  fmt.Sprintf("%s:\n  %s", summary, strings.Join(msgs, "\n  ")),
	}
}

// classifyAborted rewrites child errors that only failed because our
// fail-fast cancel() fired in response to firstFailure's genuine failure.
// It must only be called when that cancellation is known to be ours (the
// caller checks that the parent context is still alive while the run
// context is canceled) — otherwise a user Ctrl-C or an upstream deadline
// would mislabel every child as "aborted: another context failed" when no
// sibling failed at all.
//
// Under that gate every non-first child error is cancellation-induced by
// construction, regardless of its shape: some chain context.Canceled (a
// docker call that observes ctx.Done() before starting its own work), while
// others are opaque — exec.CommandContext SIGKILLs an in-flight `docker
// compose` subprocess and the resulting "signal: killed" *exec.ExitError
// carries no context.Canceled anywhere in its chain, so errors.Is matching
// alone could never recognize it.
//
// firstFailure is the context name that actually triggered cancel(); it is
// never classified as aborted — even when its error happens to chain
// context.Canceled — because it is the genuine failure by construction.
func classifyAborted(errs []ContextResult, firstFailure string) []ContextResult {
	out := make([]ContextResult, 0, len(errs))
	for _, r := range errs {
		if r.ContextName == firstFailure {
			out = append(out, r)
			continue
		}
		out = append(out, ContextResult{ContextName: r.ContextName, Err: &apperr.AbortedError{ContextName: r.ContextName, Err: r.Err}})
	}
	return out
}

