// Package llmcmd implements `dockform llm`, which prints what an AI agent
// needs to know to work with dockform.
package llmcmd

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/gcstr/dockform/internal/cli/buildinfo"
	"github.com/spf13/cobra"
)

// guide is the agent guide. It is plain Markdown with one placeholder,
// {{.Version}}, filled in with strings.Replace rather than text/template so
// the text can quote Go templates (docker --format) without escaping.
//
//go:embed guide.md
var guide string

// New creates the `llm` command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "llm",
		Short: "Print what an AI agent needs to know to work with dockform",
		Long: `Print a compact Markdown guide for AI agents: how dockform works, every
manifest key, the commands, common tasks, and rules to follow.

It is meant to be loaded into an agent's context at the start of a session.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprint(cmd.OutOrStdout(), Render())
			return err
		},
	}
}

// Render returns the guide for the running dockform version.
func Render() string {
	return strings.ReplaceAll(guide, "{{.Version}}", buildinfo.Version())
}
