package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gcstr/dockform/internal/cli"
)

func main() {
	// Create a context that cancels on SIGINT or SIGTERM
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	os.Exit(cli.Execute(ctx))
}
