package llmcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/spf13/cobra"
)

const (
	blockBegin = "<!-- BEGIN DOCKFORM (managed by dockform llm setup) -->"
	blockEnd   = "<!-- END DOCKFORM -->"
	hookCmd    = "dockform llm"
)

// agentBlock is written into AGENTS.md (and CLAUDE.md when it exists).
const agentBlock = blockBegin + `
## Dockform

This repository deploys Docker Compose stacks with dockform. Before changing
the manifest, stacks, secrets or volumes, run ` + "`dockform llm`" + ` and follow its
"Rules for agents". Show ` + "`dockform plan`" + ` before any ` + "`dockform apply`" + `.
` + blockEnd + "\n"

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup [directory]",
		Short: "Point AI agents at dockform llm: AGENTS.md, CLAUDE.md and a Claude Code session hook",
		Long: `Make AI agents working in this repository load ` + "`dockform llm`" + ` on their own:

  - AGENTS.md gets a short dockform block (created if missing).
  - CLAUDE.md gets the same block, if the file exists.
  - .claude/settings.json gets a SessionStart hook that runs ` + "`dockform llm`" + `,
    so Claude Code starts every session with it.

The block is marked, so running setup again updates it in place, and the hook
is added only once. Other content in these files is left alone.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			noHook, _ := cmd.Flags().GetBool("no-hook")
			changes, err := Setup(dir, !noHook)
			for _, c := range changes {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), c)
			}
			return err
		},
	}
	cmd.Flags().Bool("no-hook", false, "Don't add the Claude Code SessionStart hook")
	return cmd
}

// Setup writes the agent block and, with hook, the Claude Code hook into dir.
// It returns one line per file saying what happened.
func Setup(dir string, hook bool) ([]string, error) {
	var changes []string

	msg, err := upsertBlock(filepath.Join(dir, "AGENTS.md"), true)
	if err != nil {
		return changes, err
	}
	changes = append(changes, "AGENTS.md: "+msg)

	msg, err = upsertBlock(filepath.Join(dir, "CLAUDE.md"), false)
	if err != nil {
		return changes, err
	}
	if msg != "" {
		changes = append(changes, "CLAUDE.md: "+msg)
	}

	if hook {
		msg, err = addSessionHook(filepath.Join(dir, ".claude", "settings.json"))
		if err != nil {
			return changes, err
		}
		changes = append(changes, ".claude/settings.json: "+msg)
	}
	return changes, nil
}

// upsertBlock replaces the dockform block in path, or appends it. A missing
// file is created only when create is set; otherwise it returns "".
func upsertBlock(path string, create bool) (string, error) {
	const op = "llmcmd.Setup"
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if !create {
			return "", nil
		}
		if err := os.WriteFile(path, []byte(agentBlock), 0o644); err != nil {
			return "", apperr.Wrap(op, apperr.Internal, err, "write %s", path)
		}
		return "created with the dockform block", nil
	case err != nil:
		return "", apperr.Wrap(op, apperr.Internal, err, "read %s", path)
	}

	text := string(raw)
	var updated, msg string
	begin := strings.Index(text, blockBegin)
	end := strings.Index(text, blockEnd)
	switch {
	case begin >= 0 && end > begin:
		updated = text[:begin] + agentBlock + strings.TrimPrefix(text[end+len(blockEnd):], "\n")
		msg = "updated the dockform block"
	case begin >= 0 || end >= 0:
		return "", apperr.New(op, apperr.InvalidInput, "%s has only one of the dockform block markers; fix or remove it and run setup again", path)
	default:
		sep := "\n"
		if text != "" && !strings.HasSuffix(text, "\n") {
			sep = "\n\n"
		}
		if text == "" {
			sep = ""
		}
		updated = text + sep + agentBlock
		msg = "added the dockform block"
	}
	if updated == text {
		return "dockform block already up to date", nil
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return "", apperr.Wrap(op, apperr.Internal, err, "write %s", path)
	}
	return msg, nil
}

// addSessionHook adds a SessionStart hook running `dockform llm` to a Claude
// Code settings file, keeping everything else in it. The file is rewritten
// only when the hook is missing.
func addSessionHook(path string) (string, error) {
	const op = "llmcmd.Setup"
	settings := map[string]json.RawMessage{}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", apperr.Wrap(op, apperr.Internal, err, "read %s", path)
	case len(bytes.TrimSpace(raw)) > 0:
		if err := json.Unmarshal(raw, &settings); err != nil {
			return "", apperr.Wrap(op, apperr.InvalidInput, err, "%s is not valid JSON", path)
		}
	}

	type hookCommand struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type matcherGroup struct {
		Matcher string        `json:"matcher"`
		Hooks   []hookCommand `json:"hooks"`
	}
	hooks := map[string]json.RawMessage{}
	if h, ok := settings["hooks"]; ok {
		if err := json.Unmarshal(h, &hooks); err != nil {
			return "", apperr.Wrap(op, apperr.InvalidInput, err, "%s: hooks is not an object", path)
		}
	}
	var groups []json.RawMessage
	if s, ok := hooks["SessionStart"]; ok {
		if err := json.Unmarshal(s, &groups); err != nil {
			return "", apperr.Wrap(op, apperr.InvalidInput, err, "%s: hooks.SessionStart is not a list", path)
		}
	}
	for _, g := range groups {
		var mg matcherGroup
		if json.Unmarshal(g, &mg) != nil {
			continue
		}
		for _, h := range mg.Hooks {
			if strings.TrimSpace(h.Command) == hookCmd {
				return "SessionStart hook already present", nil
			}
		}
	}

	add, _ := json.Marshal(matcherGroup{Hooks: []hookCommand{{Type: "command", Command: hookCmd}}})
	groups = append(groups, add)
	hooks["SessionStart"], _ = json.Marshal(groups)
	settings["hooks"], _ = json.Marshal(hooks)

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", apperr.Wrap(op, apperr.Internal, err, "encode %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", apperr.Wrap(op, apperr.Internal, err, "create %s", filepath.Dir(path))
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return "", apperr.Wrap(op, apperr.Internal, err, "write %s", path)
	}
	return "added a SessionStart hook running `dockform llm`", nil
}
