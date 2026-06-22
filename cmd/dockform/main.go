package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/sshmux"
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
	if sshmux.ShouldRunAsShim(os.Args[0]) {
		if err := sshmux.Run(os.Args[0], os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(255)
		}
		return
	}
	os.Exit(run())
}
