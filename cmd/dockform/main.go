package main

import (
	"log"

	"github.com/gcstr/dockform/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		log.Fatal(err)
	}
}
