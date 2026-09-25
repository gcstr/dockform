package llmcmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/clitest"
)

// writeProject lays out a small homelab: discovered stacks with env, secrets
// and a fileset, context-wide secrets, a kept volume, a deployment, an
// explicit stack, and a deprecated secrets.sops.
func writeProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"hetzner-one/navidrome/compose.yaml":                  "services:\n  navidrome:\n    image: deluan/navidrome:0.64.2\n",
		"hetzner-one/navidrome/environment.env":               "ND_LOGLEVEL=info\n",
		"hetzner-one/navidrome/volumes/config/navidrome.toml": "x\n",
		"hetzner-one/paperless/compose.yaml":                  "services:\n  paperless:\n    image: paperlessngx/paperless-ngx:3.2.1\n",
		"hetzner-one/paperless/secrets.env":                   "A=1\n",
		"hetzner-one/paperless/extra.env":                     "B=1\n",
		"hetzner-two/secrets.env":                             "C=1\n",
		"hetzner-two/traefik/compose.yaml":                    "services:\n  traefik:\n    image: traefik:v3.7.13\n",
		"legacy/docker-compose.yml":                           "services:\n  app:\n    image: nginx:1\n",
		"dockform.yml": `identifier: homeserver
sops:
  age:
    key_file: ${LLM_TEST_UNSET_KEY_FILE}
contexts:
  hetzner-one:
    host: ssh://hetzner-one
  hetzner-two:
    volumes:
      letsencrypt: {destroy: false}
    networks:
      traefik: {}
deployments:
  edge:
    description: Public entrypoint
    stacks: [hetzner-two/traefik]
stacks:
  hetzner-one/paperless:
    environment:
      inline: [TZ=Europe/Amsterdam]
    secrets:
      sops: [extra.env]
  hetzner-two/legacy:
    root: legacy
    files: [docker-compose.yml]
`,
	}
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runLLM runs `dockform llm` with a fake docker that records every call and
// answers `context inspect` like a local docker would.
func runLLM(t *testing.T, args ...string) (out string, dockerCalls []string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "calls")
	defer clitest.WithCustomDockerStub(t, `#!/bin/sh
echo "$*" >> `+log+`
if [ "$1" = "context" ] && [ "$2" = "inspect" ]; then echo "ssh://two.example"; exit 0; fi
exit 1
`)()

	root := cli.TestNewRootCmd()
	var b strings.Builder
	root.SetOut(&b)
	root.SetErr(&b)
	root.SetArgs(append([]string{"llm"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("dockform llm: %v", err)
	}
	if raw, err := os.ReadFile(log); err == nil {
		dockerCalls = strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	return b.String(), dockerCalls
}

func TestLLM_ProjectSection(t *testing.T) {
	out, _ := runLLM(t, "--manifest", writeProject(t))
	section := out[strings.Index(out, "## This project"):]

	for _, want := range []string{
		"- Identifier: `homeserver`",
		"| hetzner-one | host `ssh://hetzner-one` | - | - |",
		"| hetzner-two | docker context `hetzner-two` (ssh://two.example) | letsencrypt (destroy: false) | traefik |",
		"- edge: stacks hetzner-two/traefik (Public entrypoint)",
		"| hetzner-one/navidrome | discovered | environment.env | - | config |",
		"| hetzner-one/paperless | discovered | 1 inline | secrets.env, extra.env | - |",
		"| hetzner-two/legacy | explicit, root `legacy` | - | - | - |",
		"| hetzner-two/traefik | discovered | - | - | - |",
		"Context-wide secrets: `hetzner-two/secrets.env`.",
		"environment variable LLM_TEST_UNSET_KEY_FILE is not set",
		"stack hetzner-one/paperless: secrets.sops is deprecated",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("project section missing %q\n%s", want, section)
		}
	}
}

// The project section must stay local: reading docker context metadata is
// fine, talking to a daemon is not.
func TestLLM_ProjectSectionContactsNoDaemon(t *testing.T) {
	_, calls := runLLM(t, "--manifest", writeProject(t))
	if len(calls) == 0 {
		t.Fatal("expected docker context inspect for the context without a host; the fake docker saw nothing")
	}
	for _, c := range calls {
		if !strings.HasPrefix(c, "context inspect") {
			t.Errorf("dockform llm ran a docker command other than context inspect: %q", c)
		}
	}
}

func TestLLM_NoProject(t *testing.T) {
	out, calls := runLLM(t, "--manifest", writeProject(t), "--no-project")
	if strings.Contains(out, "## This project") || len(calls) != 0 {
		t.Errorf("--no-project should print only the guide and run no docker, got calls %q", calls)
	}
}

func TestLLM_NoManifest(t *testing.T) {
	out, _ := runLLM(t, "--manifest", t.TempDir())
	if !strings.Contains(out, "No dockform manifest here.") {
		t.Errorf("expected a note that there is no manifest, got:\n%s", out[strings.LastIndex(out, "##"):])
	}
}

func TestLLM_BrokenManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dockform.yml"), []byte("identifier: x\ncontexts: {}\nbogus: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := runLLM(t, "--manifest", dir)
	if !strings.Contains(out, "The manifest failed to load, so fix it first:") {
		t.Errorf("expected the load error in the project section, got:\n%s", out[strings.LastIndex(out, "##"):])
	}
}
