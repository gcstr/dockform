package versioncmd

import (
	"fmt"
	"runtime"

	"github.com/gcstr/dockform/internal/cli/buildinfo"
	"github.com/spf13/cobra"
)

// New creates the `version` command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show detailed version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Dockform\n")
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), " Version:\t%s\n", buildinfo.Version())

			// Show Go version (runtime or build-time if available)
			goVer := buildinfo.GoVersion()
			if goVer == "" {
				goVer = runtime.Version()
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), " Go version:\t%s\n", goVer)

			// Show commit/date/builder if available via buildinfo
			commit := buildinfo.Commit()
			if commit == "" {
				commit = "<unknown>"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), " Git commit:\t%s\n", commit)

			built := buildinfo.BuildDate()
			if built == "" {
				built = "<unknown>"
			}
			if by := buildinfo.BuiltBy(); by != "" && built != "<unknown>" {
				built = built + " (" + by + ")"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), " Built:\t\t%s\n", built)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), " OS/Arch:\t%s/%s\n", runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
	return cmd
}
