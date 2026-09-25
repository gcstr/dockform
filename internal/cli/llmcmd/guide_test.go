package llmcmd_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/cli"
	"github.com/gcstr/dockform/internal/cli/llmcmd"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/spf13/cobra"
)

// The guide is hand-written, so these tests are what keep it honest: a new
// manifest key or command fails CI until the guide describes it.

// yamlBlockAfter returns the first ```yaml block after marker in the guide.
func yamlBlockAfter(t *testing.T, marker string) string {
	t.Helper()
	guide := llmcmd.Render()
	i := strings.Index(guide, marker)
	if i < 0 {
		t.Fatalf("guide has no %q", marker)
	}
	m := regexp.MustCompile("(?s)```yaml\n(.*?)```").FindStringSubmatch(guide[i:])
	if m == nil {
		t.Fatalf("guide has no ```yaml block after %q", marker)
	}
	return m[1]
}

// manifestExample returns the guide's annotated dockform.yml.
func manifestExample(t *testing.T) string {
	return yamlBlockAfter(t, "## Manifest reference")
}

// yamlKeys collects every yaml key reachable from t.
func yamlKeys(t reflect.Type, seen map[reflect.Type]bool, keys map[string]bool) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return
	}
	seen[t] = true
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		keys[name] = true
		yamlKeys(f.Type, seen, keys)
	}
}

func TestGuide_ManifestExampleNamesEveryKey(t *testing.T) {
	example := manifestExample(t)
	keys := map[string]bool{}
	yamlKeys(reflect.TypeOf(manifest.Config{}), map[reflect.Type]bool{}, keys)

	var missing []string
	for k := range keys {
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(k) + `\b`).MatchString(example) {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("guide.md's manifest example doesn't mention these keys: %v", missing)
	}
}

func TestGuide_ListsEveryCommand(t *testing.T) {
	guide := llmcmd.Render()
	var missing []string
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || !sub.IsAvailableCommand() || sub.Name() == "help" || sub.Name() == "completion" || sub.Name() == "version" {
				continue
			}
			path := strings.TrimPrefix(sub.CommandPath(), "dockform ")
			if !strings.Contains(guide, "`"+path+" ") && !strings.Contains(guide, "`"+path+"`") && !strings.Contains(guide, "dockform "+path) {
				missing = append(missing, path)
			}
			walk(sub)
		}
	}
	walk(cli.TestNewRootCmd())
	if len(missing) > 0 {
		t.Errorf("guide.md doesn't list these commands: %v", missing)
	}
}

// The example must be a manifest dockform actually accepts.
func TestGuide_ManifestExampleLoads(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "project")
	files := map[string]string{
		"project/dockform.yml":                           manifestExample(t),
		"project/hetzner-one/navidrome/compose.yaml":     "services:\n  navidrome:\n    image: deluan/navidrome:0.64.2\n",
		"project/hetzner-one/navidrome/volumes/config/x": "x\n",
		"project/hetzner-two/traefik/compose.yaml":       "services:\n  traefik:\n    image: traefik:v3\n",
		// ../apps/legacy from the project directory.
		"apps/legacy/docker-compose.yml":      "services:\n  app:\n    image: nginx:1\n",
		"apps/legacy/docker-compose.prod.yml": "services: {}\n",
		"apps/legacy/app.env":                 "A=1\n",
		"apps/legacy/secrets/app.env":         "S=1\n",
		"age.key":                             "# age key\n",
	}
	for rel, body := range files {
		p := filepath.Join(base, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AGE_KEY_FILE", filepath.Join(base, "age.key"))

	cfg, missing, err := manifest.LoadWithWarnings(filepath.Join(project, "dockform.yml"))
	if err != nil {
		t.Fatalf("the guide's manifest example doesn't load: %v", err)
	}
	if len(missing) > 0 {
		t.Errorf("example uses unset variables: %v", missing)
	}
	if w := cfg.DeprecationWarnings(); len(w) > 0 {
		t.Errorf("example should not use deprecated settings: %v", w)
	}
}

func TestGuide_NoEmDashes(t *testing.T) {
	if strings.Contains(llmcmd.Render(), "—") {
		t.Error("guide.md contains an em dash")
	}
}

func TestLLMCommand_PrintsGuide(t *testing.T) {
	root := cli.TestNewRootCmd()
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"llm"})
	if err := root.Execute(); err != nil {
		t.Fatalf("dockform llm: %v", err)
	}
	if out.String() != llmcmd.Render() {
		t.Error("dockform llm should print the rendered guide")
	}
	if strings.Contains(out.String(), "{{.Version}}") {
		t.Error("the version placeholder was not filled in")
	}
}
