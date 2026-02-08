package planner

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

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
