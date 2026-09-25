package llmcmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/llmcmd"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sessionStartCommands lists every SessionStart hook command in a settings file.
func sessionStartCommands(t *testing.T, path string) []string {
	t.Helper()
	var s struct {
		Hooks struct {
			SessionStart []struct {
				Hooks []struct{ Command string } `json:"hooks"`
			} `json:"SessionStart"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(read(t, path)), &s); err != nil {
		t.Fatalf("settings is not valid JSON: %v", err)
	}
	var cmds []string
	for _, g := range s.Hooks.SessionStart {
		for _, h := range g.Hooks {
			cmds = append(cmds, h.Command)
		}
	}
	return cmds
}

func TestSetup_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := llmcmd.Setup(dir, true); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	agents := read(t, filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(agents, "run `dockform llm`") || !strings.Contains(agents, "<!-- END DOCKFORM -->") {
		t.Errorf("AGENTS.md should hold the dockform block, got:\n%s", agents)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("CLAUDE.md must only be updated when it already exists")
	}
	if cmds := sessionStartCommands(t, filepath.Join(dir, ".claude", "settings.json")); len(cmds) != 1 || cmds[0] != "dockform llm" {
		t.Errorf("expected one SessionStart hook running dockform llm, got %q", cmds)
	}
}

// The realistic case: a repo already set up for beads.
func TestSetup_KeepsExistingContentAndHooks(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "AGENTS.md"), "# Agents\n\n<!-- BEGIN BEADS INTEGRATION -->\nuse bd\n<!-- END BEADS INTEGRATION -->\n")
	write(t, filepath.Join(dir, "CLAUDE.md"), "# Project notes") // no trailing newline
	write(t, filepath.Join(dir, ".claude", "settings.json"), `{
  "permissions": {"allow": ["Bash(go test:*)"]},
  "hooks": {
    "SessionStart": [{"matcher": "", "hooks": [{"type": "command", "command": "bd prime --hook-json"}]}],
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "guard"}]}]
  }
}`)
	if _, err := llmcmd.Setup(dir, true); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	agents := read(t, filepath.Join(dir, "AGENTS.md"))
	if !strings.HasPrefix(agents, "# Agents\n\n<!-- BEGIN BEADS INTEGRATION -->\nuse bd\n<!-- END BEADS INTEGRATION -->\n\n<!-- BEGIN DOCKFORM") {
		t.Errorf("existing AGENTS.md content must stay, with the block appended after it:\n%s", agents)
	}
	if claude := read(t, filepath.Join(dir, "CLAUDE.md")); !strings.HasPrefix(claude, "# Project notes\n\n<!-- BEGIN DOCKFORM") {
		t.Errorf("CLAUDE.md should get the block after its content:\n%s", claude)
	}

	settings := filepath.Join(dir, ".claude", "settings.json")
	if cmds := sessionStartCommands(t, settings); len(cmds) != 2 || cmds[0] != "bd prime --hook-json" || cmds[1] != "dockform llm" {
		t.Errorf("the beads hook must stay and dockform's be added, got %q", cmds)
	}
	s := read(t, settings)
	if !strings.Contains(s, `"Bash(go test:*)"`) || !strings.Contains(s, `"guard"`) {
		t.Errorf("other settings must be kept:\n%s", s)
	}
}

func TestSetup_RunTwiceChangesNothing(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "CLAUDE.md"), "# Notes\n")
	if _, err := llmcmd.Setup(dir, true); err != nil {
		t.Fatal(err)
	}
	paths := []string{"AGENTS.md", "CLAUDE.md", ".claude/settings.json"}
	before := map[string]string{}
	for _, p := range paths {
		before[p] = read(t, filepath.Join(dir, p))
	}

	changes, err := llmcmd.Setup(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if read(t, filepath.Join(dir, p)) != before[p] {
			t.Errorf("%s changed on the second run", p)
		}
	}
	want := []string{"AGENTS.md: dockform block already up to date", "CLAUDE.md: dockform block already up to date", ".claude/settings.json: SessionStart hook already present"}
	if strings.Join(changes, "\n") != strings.Join(want, "\n") {
		t.Errorf("second run should report nothing to do, got %q", changes)
	}
}

func TestSetup_ReplacesAnOlderBlockInPlace(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "AGENTS.md"), "before\n\n<!-- BEGIN DOCKFORM (managed by dockform llm setup) -->\nold text\n<!-- END DOCKFORM -->\n\nafter\n")
	if _, err := llmcmd.Setup(dir, false); err != nil {
		t.Fatal(err)
	}
	agents := read(t, filepath.Join(dir, "AGENTS.md"))
	if strings.Contains(agents, "old text") || !strings.HasPrefix(agents, "before\n\n<!-- BEGIN DOCKFORM") || !strings.HasSuffix(agents, "<!-- END DOCKFORM -->\n\nafter\n") {
		t.Errorf("the block should be replaced where it was:\n%s", agents)
	}
	if strings.Count(agents, "BEGIN DOCKFORM") != 1 {
		t.Errorf("expected exactly one block:\n%s", agents)
	}
}

func TestSetup_NoHook(t *testing.T) {
	dir := t.TempDir()
	if _, err := llmcmd.Setup(dir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
		t.Error("without the hook, .claude/ must not be created")
	}
}

func TestSetup_RefusesToGuess(t *testing.T) {
	t.Run("invalid settings JSON", func(t *testing.T) {
		dir := t.TempDir()
		settings := filepath.Join(dir, ".claude", "settings.json")
		write(t, settings, "{ not json")
		if _, err := llmcmd.Setup(dir, true); err == nil {
			t.Fatal("expected an error for invalid JSON")
		}
		if read(t, settings) != "{ not json" {
			t.Error("an invalid settings file must be left untouched")
		}
	})
	t.Run("half a block", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "AGENTS.md"), "<!-- BEGIN DOCKFORM (managed by dockform llm setup) -->\nno end marker\n")
		if _, err := llmcmd.Setup(dir, true); err == nil {
			t.Fatal("expected an error when only one marker is present")
		}
	})
}

func TestLLMSetupCommand(t *testing.T) {
	dir := t.TempDir()
	root := cli.TestNewRootCmd()
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"llm", "setup", dir})
	if err := root.Execute(); err != nil {
		t.Fatalf("dockform llm setup: %v", err)
	}
	for _, want := range []string{"AGENTS.md: created with the dockform block", ".claude/settings.json: added a SessionStart hook"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}
