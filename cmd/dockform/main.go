package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gcstr/dockform/internal/cli"
)

var (
	execCLI      = cli.Execute
	notifySignal = signal.Notify
)

func run() int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	notifySignal(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	return execCLI(ctx)
}

func main() {
	os.Exit(run())
}
