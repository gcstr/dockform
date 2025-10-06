package cli

import (
	"github.com/gcstr/dockform/internal/cli/buildinfo"
)

// Version wrapper for tests and other packages referencing cli.Version().
func Version() string { return buildinfo.Version() }
