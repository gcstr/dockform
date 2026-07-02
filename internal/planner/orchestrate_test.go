package planner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
)

func twoContextConfig() *manifest.Config {
	return &manifest.Config{
		Identifier: "test",
		Contexts: map[string]manifest.ContextConfig{
			"alpha": {},
			"beta":  {},
		},
	}
}

func TestExecuteAcrossContexts_Sequential(t *testing.T) {
	p := New().WithParallel(false)
	cfg := twoContextConfig()

	var order []string
	err := p.ExecuteAcrossContexts(context.Background(), cfg, func(_ context.Context, name string) error {
		order = append(order, name)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Sorted order: alpha, beta
	if len(order) != 2 || order[0] != "alpha" || order[1] != "beta" {
		t.Fatalf("expected [alpha, beta], got %v", order)
	}
}

func TestExecuteAcrossContexts_SequentialStopsOnError(t *testing.T) {
	p := New().WithParallel(false)
	cfg := twoContextConfig()

	var count int
	err := p.ExecuteAcrossContexts(context.Background(), cfg, func(_ context.Context, name string) error {
		count++
		return fmt.Errorf("fail on %s", name)
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if count != 1 {
		t.Fatalf("expected 1 call (stop on first error), got %d", count)
	}
}

func TestExecuteAcrossContexts_Parallel(t *testing.T) {
	p := New().WithParallel(true)
	cfg := twoContextConfig()

	var running int64
	var maxConcurrent int64

	err := p.ExecuteAcrossContexts(context.Background(), cfg, func(_ context.Context, name string) error {
		cur := atomic.AddInt64(&running, 1)
		// Track max concurrency
		for {
			old := atomic.LoadInt64(&maxConcurrent)
			if cur <= old || atomic.CompareAndSwapInt64(&maxConcurrent, old, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt64(&running, -1)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt64(&maxConcurrent) < 2 {
		t.Fatal("expected parallel execution (concurrency >= 2)")
	}
}

func TestExecuteAcrossContexts_ParallelCollectsErrors(t *testing.T) {
	p := New().WithParallel(true)
	cfg := twoContextConfig()

	err := p.ExecuteAcrossContexts(context.Background(), cfg, func(_ context.Context, name string) error {
		return fmt.Errorf("fail on %s", name)
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestExecuteAcrossContexts_ParallelPreservesChildCauses verifies that when
// multiple contexts fail, the aggregate error keeps each child's underlying
// cause reachable (via apperr.MultiError + apperr.ContextError) instead of
// pre-stringifying them with %v and discarding the cause, as
// printUserFriendly relies on this to surface e.g. captured compose stderr.
func TestExecuteAcrossContexts_ParallelPreservesChildCauses(t *testing.T) {
	p := New().WithParallel(true)
	cfg := twoContextConfig()

	err := p.ExecuteAcrossContexts(context.Background(), cfg, func(_ context.Context, name string) error {
		leaf := &apperr.E{
			Op:   "dockercli.Exec",
			Kind: apperr.External,
			Err:  errors.New("exit status 1"),
			Msg:  fmt.Sprintf("manifest for example/%s:1.0 not found: manifest unknown", name),
		}
		return apperr.Wrap("planner.Apply", apperr.External, leaf, "compose up %s/stack", name)
	})
	if err == nil {
		t.Fatal("expected aggregate error")
	}

	var e *apperr.E
	if !errors.As(err, &e) {
		t.Fatalf("expected *apperr.E, got %T", err)
	}
	var multi *apperr.MultiError
	if !errors.As(e.Err, &multi) {
		t.Fatalf("expected aggregate Err to be *apperr.MultiError, got %T", e.Err)
	}
	if len(multi.Errors) != 2 {
		t.Fatalf("expected 2 child errors, got %d", len(multi.Errors))
	}

	seen := map[string]bool{}
	for _, child := range multi.Errors {
		var ctxErr *apperr.ContextError
		if !errors.As(child, &ctxErr) {
			t.Fatalf("expected child to be *apperr.ContextError, got %T", child)
		}
		seen[ctxErr.ContextName] = true
		detail := apperr.DeepestMessage(child)
		want := fmt.Sprintf("manifest for example/%s:1.0 not found: manifest unknown", ctxErr.ContextName)
		if detail != want {
			t.Fatalf("expected deepest message %q for context %s, got %q", want, ctxErr.ContextName, detail)
		}
	}
	if !seen["alpha"] || !seen["beta"] {
		t.Fatalf("expected both alpha and beta contexts present, got %v", seen)
	}
}

func TestExecuteAcrossContexts_EmptyContexts(t *testing.T) {
	p := New()
	cfg := &manifest.Config{Identifier: "test", Contexts: map[string]manifest.ContextConfig{}}

	err := p.ExecuteAcrossContexts(context.Background(), cfg, func(_ context.Context, name string) error {
		t.Fatal("should not be called")
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteAcrossContexts_ContextCancellation(t *testing.T) {
	p := New().WithParallel(false)
	cfg := twoContextConfig()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := p.ExecuteAcrossContexts(ctx, cfg, func(_ context.Context, name string) error {
		t.Fatal("should not be called")
		return nil
	})
	if err == nil {
		t.Fatal("expected context error")
	}
}

// TestExecuteAcrossContextsMode_RunToCompletion_NoSiblingCancellation verifies
// the mutating-mode contract: when one context fails, every other context
// still runs to completion (its work is not interrupted), and a sibling that
// completes successfully does not appear anywhere in the error aggregate.
func TestExecuteAcrossContextsMode_RunToCompletion_NoSiblingCancellation(t *testing.T) {
	p := New().WithParallel(true)
	cfg := twoContextConfig()

	release := make(chan struct{})
	var betaCompleted atomic.Bool
	var betaSawCancel atomic.Bool

	err := p.ExecuteAcrossContextsMode(context.Background(), cfg, RunToCompletion, func(ctx context.Context, name string) error {
		if name == "alpha" {
			// Fail immediately.
			return fmt.Errorf("bad image tag on alpha")
		}
		// beta: simulate a long-running docker compose up that must not be
		// killed just because alpha failed.
		select {
		case <-ctx.Done():
			betaSawCancel.Store(true)
		case <-time.After(100 * time.Millisecond):
		}
		close(release)
		betaCompleted.Store(true)
		return nil
	})

	<-release

	if !betaCompleted.Load() {
		t.Fatal("expected beta to run to completion despite alpha's failure")
	}
	if betaSawCancel.Load() {
		t.Fatal("expected beta's context to never be canceled in RunToCompletion mode")
	}

	if err == nil {
		t.Fatal("expected an error since alpha failed")
	}
	var e *apperr.E
	if errors.As(err, &e) {
		var multi *apperr.MultiError
		if errors.As(e.Err, &multi) {
			t.Fatalf("expected a single genuine failure (alpha only), got MultiError with %d entries", len(multi.Errors))
		}
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("expected error to reference alpha, got: %v", err)
	}
	if strings.Contains(err.Error(), "beta") {
		t.Fatalf("beta succeeded and must not appear in the error aggregate, got: %v", err)
	}
}

// TestExecuteAcrossContextsMode_FailFast_DistinguishesAbortedFromGenuine
// covers the read-only/discovery fail-fast path: one context fails for a
// real reason, a sibling is canceled as a side effect and returns an opaque
// "killed" style error (mirroring exec.CommandContext's SIGKILL behavior,
// which carries no context.Canceled in its chain). The aggregate must
// clearly distinguish the two: exactly one genuine failure, and the
// cancellation-induced error must not be presented as a failure.
func TestExecuteAcrossContextsMode_FailFast_DistinguishesAbortedFromGenuine(t *testing.T) {
	p := New().WithParallel(true)
	cfg := twoContextConfig()

	err := p.ExecuteAcrossContextsMode(context.Background(), cfg, FailFast, func(ctx context.Context, name string) error {
		if name == "alpha" {
			return apperr.New("planner.Discover", apperr.External, "bad image tag")
		}
		// beta: block until canceled, then return an opaque error with no
		// context.Canceled in its chain, mirroring "signal: killed" from a
		// SIGKILLed docker compose subprocess.
		<-ctx.Done()
		return errors.New("signal: killed")
	})

	if err == nil {
		t.Fatal("expected an aggregate error")
	}

	var e *apperr.E
	if !errors.As(err, &e) {
		t.Fatalf("expected *apperr.E, got %T", err)
	}
	var multi *apperr.MultiError
	if !errors.As(e.Err, &multi) {
		t.Fatalf("expected *apperr.MultiError, got %T", e.Err)
	}
	if len(multi.Errors) != 2 {
		t.Fatalf("expected 2 child entries (1 genuine + 1 aborted), got %d", len(multi.Errors))
	}

	genuineCount := 0
	abortedCount := 0
	for _, child := range multi.Errors {
		var ctxErr *apperr.ContextError
		if !errors.As(child, &ctxErr) {
			t.Fatalf("expected *apperr.ContextError, got %T", child)
		}
		if apperr.IsAborted(child) {
			abortedCount++
			if ctxErr.ContextName != "beta" {
				t.Fatalf("expected beta to be the aborted context, got %s", ctxErr.ContextName)
			}
		} else {
			genuineCount++
			if ctxErr.ContextName != "alpha" {
				t.Fatalf("expected alpha to be the genuine failure, got %s", ctxErr.ContextName)
			}
		}
	}
	if genuineCount != 1 {
		t.Fatalf("expected exactly 1 genuine failure, got %d", genuineCount)
	}
	if abortedCount != 1 {
		t.Fatalf("expected exactly 1 aborted context, got %d", abortedCount)
	}

	// The top-level summary message must not word the aborted context as a
	// failure alongside the genuine one.
	if !strings.Contains(e.Msg, "1 context(s) failed") {
		t.Fatalf("expected summary to report exactly 1 failed context, got: %s", e.Msg)
	}
	if !strings.Contains(e.Msg, "aborted") {
		t.Fatalf("expected summary to call out the aborted context, got: %s", e.Msg)
	}
}

// TestExecuteAcrossContextsMode_FailFast_ParentCancelNotAborted verifies
// that cancellation arriving via the PARENT context (user Ctrl-C, upstream
// deadline) is never misattributed to a sibling failure: when the parent is
// canceled while both children are in flight and both return
// context.Canceled-chained errors, no child may be wrapped in AbortedError
// and the aggregate must not claim a sibling failed.
func TestExecuteAcrossContextsMode_FailFast_ParentCancelNotAborted(t *testing.T) {
	p := New().WithParallel(true)
	cfg := twoContextConfig()

	parentCtx, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	var inFlight sync.WaitGroup
	inFlight.Add(2)
	go func() {
		// Cancel the parent only once both children are in flight.
		inFlight.Wait()
		cancelParent()
	}()

	err := p.ExecuteAcrossContextsMode(parentCtx, cfg, FailFast, func(ctx context.Context, name string) error {
		inFlight.Done()
		<-ctx.Done()
		return fmt.Errorf("docker compose config for %s: %w", name, context.Canceled)
	})

	if err == nil {
		t.Fatal("expected an error since both children were canceled")
	}
	if apperr.IsAborted(err) {
		t.Fatalf("no child may be classified as aborted when the parent was canceled, got: %v", err)
	}
	var e *apperr.E
	if errors.As(err, &e) {
		var multi *apperr.MultiError
		if errors.As(e.Err, &multi) {
			for _, child := range multi.Errors {
				if apperr.IsAborted(child) {
					t.Fatalf("child wrongly classified as aborted on parent cancellation: %v", child)
				}
				if !errors.Is(child, context.Canceled) {
					t.Fatalf("expected child to preserve context.Canceled chain, got: %v", child)
				}
			}
		}
	}
	if strings.Contains(err.Error(), "aborted") {
		t.Fatalf("aggregate must not mention sibling-induced aborts on parent cancellation, got: %v", err)
	}
}

func TestExecuteAcrossContexts_SingleContextRunsSequential(t *testing.T) {
	p := New().WithParallel(true) // parallel enabled but only 1 context
	cfg := &manifest.Config{
		Identifier: "test",
		Contexts: map[string]manifest.ContextConfig{
			"only": {},
		},
	}

	called := false
	err := p.ExecuteAcrossContexts(context.Background(), cfg, func(_ context.Context, name string) error {
		called = true
		if name != "only" {
			t.Fatalf("expected 'only', got %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected fn to be called")
	}
}
