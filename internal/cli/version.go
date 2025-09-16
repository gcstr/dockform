package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show detailed version information",
		Args:  cobra.NoArgs,
		RunE:  runVersion,
	}

	return cmd
}

func runVersion(cmd *cobra.Command, args []string) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Dockform\n")
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), " Version:\t%s\n", version)

	// Show Go version (runtime or build-time if available)
	goVer := goVersion
	if goVer == "" {
		goVer = runtime.Version()
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), " Go version:\t%s\n", goVer)

	// Show commit if available
	if commit != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), " Git commit:\t%s\n", commit)
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), " Git commit:\t<unknown>\n")
	}

	// Show build date if available
	if date != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), " Built:\t\t%s\n", date)
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), " Built:\t\t<unknown>\n")
	}

	// Show OS/Arch
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), " OS/Arch:\t%s/%s\n", runtime.GOOS, runtime.GOARCH)

	return nil
}
