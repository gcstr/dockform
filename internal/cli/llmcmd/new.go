// Package llmcmd implements `dockform llm`, which prints what an AI agent
// needs to know to work with dockform.
package llmcmd

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/cli/buildinfo"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/manifest"
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
	cmd := &cobra.Command{
		Use:   "llm",
		Short: "Print what an AI agent needs to know to work with dockform",
		Long: `Print a compact Markdown guide for AI agents: how dockform works, every
manifest key, the commands, common tasks, and rules to follow.

Run inside a project, it ends with a "This project" section built from the
manifest and the stack directories: contexts, stacks, env and secrets files,
filesets, and warnings. That section is read locally; no Docker host is
contacted.

It is meant to be loaded into an agent's context at the start of a session.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := Render()
			if noProject, _ := cmd.Flags().GetBool("no-project"); !noProject {
				out += projectSection(cmd)
			}
			_, err := fmt.Fprint(cmd.OutOrStdout(), out)
			return err
		},
	}
	cmd.Flags().Bool("no-project", false, "Leave out the \"This project\" section")
	return cmd
}

// projectSection loads the manifest (--manifest, else the current directory)
// without prompting and renders the "This project" section, or a short note
// when there is no manifest or it doesn't load.
func projectSection(cmd *cobra.Command) string {
	path, _ := cmd.Flags().GetString("manifest")
	cfg, missing, err := manifest.LoadWithWarnings(path)
	switch {
	case apperr.IsKind(err, apperr.NotFound):
		return "\n## This project\n\nNo dockform manifest here. Run `dockform llm` in a project directory, or pass `--manifest`.\n"
	case err != nil:
		return "\n## This project\n\nThe manifest failed to load, so fix it first:\n\n```\n" + apperr.DeepestMessage(err) + "\n```\n"
	}

	shown := path
	if shown == "" {
		shown = "dockform.yml"
		for _, name := range []string{"dockform.yml", "dockform.yaml", "Dockform.yml", "Dockform.yaml"} {
			if _, statErr := os.Stat(name); statErr == nil {
				shown = name
				break
			}
		}
	}

	var named []string
	for name, cc := range cfg.Contexts {
		if cc.Host == "" {
			named = append(named, name)
		}
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return renderProject(projectInput{
		cfg:          &cfg,
		manifestPath: shown,
		missing:      missing,
		endpoints:    common.DockerContextEndpoints(ctx, named),
	})
}

// Render returns the guide for the running dockform version.
func Render() string {
	return strings.ReplaceAll(guide, "{{.Version}}", buildinfo.Version())
}
