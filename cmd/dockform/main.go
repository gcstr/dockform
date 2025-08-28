package main

import (
	"os"

	"github.com/gcstr/dockform/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
