package llmcmd_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/gcstr/dockform/internal/cli/llmcmd"
	"github.com/gcstr/dockform/internal/manifest"
)

// yamlBlockStarting returns the ```yaml block whose first line is marker.
func yamlBlockStarting(t *testing.T, marker string) string {
	t.Helper()
	m := regexp.MustCompile("(?s)```yaml\n(" + regexp.QuoteMeta(marker) + "\n.*?)```").FindStringSubmatch(llmcmd.Render())
	if m == nil {
		t.Fatalf("guide has no ```yaml block starting with %q", marker)
	}
	return m[1]
}

// The worked stack in "Writing compose files for dockform" must be one that
// works: built from the guide's own blocks, the manifest loads, the fileset
// lands in the volume compose mounts as external, the external network is
// the one the manifest declares, and every variable the service lists comes
// from a file in the tree.
func TestGuide_WorkedStackIsConsistent(t *testing.T) {
	composeYAML := yamlBlockStarting(t, "# hetzner-one/web/compose.yaml")
	manifestYAML := yamlBlockStarting(t, "# dockform.yml")

	dir := t.TempDir()
	stack := filepath.Join(dir, "hetzner-one", "web")
	for rel, body := range map[string]string{
		"dockform.yml":                                manifestYAML,
		"hetzner-one/web/compose.yaml":                composeYAML,
		"hetzner-one/web/environment.env":             "SITE_NAME=Example\n",
		"hetzner-one/web/secrets.env":                 "DB_PASSWORD=x\n",
		"hetzner-one/web/volumes/config/default.conf": "server {}\n",
	} {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg, _, err := manifest.LoadWithWarnings(filepath.Join(dir, "dockform.yml"))
	if err != nil {
		t.Fatalf("worked example's manifest doesn't load: %v", err)
	}
	fs, ok := cfg.GetAllFilesets()["hetzner-one/web/config"]
	if !ok {
		t.Fatalf("fileset config not discovered; got %v", cfg.GetAllFilesets())
	}

	var compose struct {
		Services map[string]struct {
			Environment []string `yaml:"environment"`
			Volumes     []string `yaml:"volumes"`
			Networks    []string `yaml:"networks"`
		} `yaml:"services"`
		Volumes  map[string]struct{ External bool } `yaml:"volumes"`
		Networks map[string]struct{ External bool } `yaml:"networks"`
	}
	if err := yaml.Unmarshal([]byte(composeYAML), &compose); err != nil {
		t.Fatalf("worked example's compose.yaml doesn't parse: %v", err)
	}
	if !compose.Volumes[fs.TargetVolume].External {
		t.Errorf("compose must declare the fileset volume %q as external", fs.TargetVolume)
	}
	web := compose.Services["web"]
	if len(web.Volumes) == 0 || !strings.HasPrefix(web.Volumes[0], fs.TargetVolume+":") {
		t.Errorf("the service should mount %q, got %v", fs.TargetVolume, web.Volumes)
	}
	for _, n := range web.Networks {
		if !compose.Networks[n].External {
			t.Errorf("network %q must be external in compose", n)
		}
		if _, declared := cfg.Contexts["hetzner-one"].Networks[n]; !declared {
			t.Errorf("network %q must be declared under contexts.hetzner-one.networks", n)
		}
	}

	// Every listed variable is defined by the stack's env or secrets file.
	defined := map[string]bool{}
	for _, f := range []string{"environment.env", "secrets.env"} {
		b, _ := os.ReadFile(filepath.Join(stack, f))
		for _, line := range strings.Split(string(b), "\n") {
			if k, _, ok := strings.Cut(line, "="); ok {
				defined[k] = true
			}
		}
	}
	for _, e := range web.Environment {
		name := regexp.MustCompile(`^[A-Z_][A-Z0-9_]*`).FindString(e)
		if !defined[name] {
			t.Errorf("environment entry %q isn't defined by environment.env or secrets.env", e)
		}
	}
}
