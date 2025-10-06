package cli

import (
	"github.com/gcstr/dockform/internal/cli/buildinfo"
	"github.com/gcstr/dockform/internal/cli/versioncmd"
	"github.com/spf13/cobra"
)

// newVersionCmd is kept for tests that call it directly.
func newVersionCmd() *cobra.Command { return versioncmd.New() }

// Version wrapper for tests and other packages referencing cli.Version().
func Version() string { return buildinfo.Version() }
