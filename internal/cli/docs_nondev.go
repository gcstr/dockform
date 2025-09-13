//go:build !dev

package cli

import "github.com/spf13/cobra"

// registerDocsCmd is a no-op outside dev builds.
func registerDocsCmd(root *cobra.Command) {}
