package planner

import (
	"context"
	"errors"
	"fmt"
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
