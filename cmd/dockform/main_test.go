package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
)

func TestRunCancelsOnSignal(t *testing.T) {
	var mu sync.Mutex
	var capturedCtx context.Context
	execCLI = func(ctx context.Context) int {
		mu.Lock()
		capturedCtx = ctx
		mu.Unlock()
		<-ctx.Done()
		return 99
	}
	notifySignal = func(c chan<- os.Signal, sig ...os.Signal) {
		go func() {
			c <- syscall.SIGTERM
		}()
	}
	defer func() {
		execCLI = cli.Execute
		notifySignal = signal.Notify
	}()

	code := run()
	if code != 99 {
		t.Fatalf("expected exit code 99, got %d", code)
	}
	mu.Lock()
	ctx := capturedCtx
	mu.Unlock()
	if ctx == nil {
		t.Fatalf("expected context to be captured")
	}
	if ctx.Err() == nil {
		t.Fatalf("expected context to be canceled")
	}
}
