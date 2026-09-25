package llmcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gcstr/dockform/internal/manifest"
)

// projectInput is everything the "This project" section is built from. It is
// all local: the loaded manifest, the stack directories discovery read, and
// docker context endpoints from local docker metadata. No daemon is contacted.
type projectInput struct {
	cfg          *manifest.Config
	manifestPath string            // as the user would type it
	missing      []string          // ${VAR}s that were unset during interpolation
	endpoints    map[string]string // docker context name → endpoint, when known
}

// renderProject renders the "This project" section.
func renderProject(in projectInput) string {
	cfg := in.cfg
	var b strings.Builder
	b.WriteString("\n## This project\n\n")
	b.WriteString("From the manifest and the stack directories. No Docker host was contacted.\n\n")
	fmt.Fprintf(&b, "- Manifest: `%s`\n", in.manifestPath)
	fmt.Fprintf(&b, "- Identifier: `%s`\n", cfg.Identifier)
	fmt.Fprintf(&b, "- SOPS: %s\n", sopsSummary(cfg.Sops))

	// Contexts.
	b.WriteString("\n| Context | Reached through | Volumes | Networks |\n|---|---|---|---|\n")
	var contextSecrets []string
	for _, name := range sortedKeys(cfg.Contexts) {
		cc := cfg.Contexts[name]
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", name, reachedThrough(name, cc, in.endpoints),
			keptList(cc.Volumes, func(v manifest.TopLevelResourceSpec) bool { return v.Kept() }),
			keptList(cc.Networks, func(n manifest.NetworkSpec) bool { return n.Kept() }))
		if p := contextSecretsPath(cfg, name); p != "" {
			contextSecrets = append(contextSecrets, "`"+rel(cfg.BaseDir, p)+"`")
		}
	}

	// Deployments.
	if len(cfg.Deployments) > 0 {
		b.WriteString("\nDeployments (`--deployment <name>`):\n")
		for _, name := range sortedKeys(cfg.Deployments) {
			d := cfg.Deployments[name]
			var parts []string
			if len(d.Contexts) > 0 {
				parts = append(parts, "contexts "+strings.Join(d.Contexts, ", "))
			}
			if len(d.Stacks) > 0 {
				parts = append(parts, "stacks "+strings.Join(d.Stacks, ", "))
			}
			line := fmt.Sprintf("- %s: %s", name, strings.Join(parts, "; "))
			if d.Description != "" {
				line += " (" + d.Description + ")"
			}
			b.WriteString(line + "\n")
		}
	}

	// Stacks.
	stacks := cfg.GetAllStacks()
	filesets := map[string][]string{} // context/stack → fileset names
	for key := range cfg.GetAllFilesets() {
		if i := strings.LastIndex(key, "/"); i > 0 {
			filesets[key[:i]] = append(filesets[key[:i]], key[i+1:])
		}
	}
	b.WriteString("\n| Stack | Kind | Env | Secrets | Filesets |\n|---|---|---|---|---|\n")
	for _, key := range sortedKeys(stacks) {
		s := stacks[key]
		kind := "discovered"
		if _, ok := cfg.DiscoveredStacks[key]; !ok {
			kind = "explicit, root `" + rel(cfg.BaseDir, s.Root) + "`"
		}
		sort.Strings(filesets[key])
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", key, kind, envSummary(s),
			orDash(stackSecrets(cfg, s)), orDash(filesets[key]))
	}
	if len(contextSecrets) > 0 {
		fmt.Fprintf(&b, "\nContext-wide secrets: %s.\n", strings.Join(contextSecrets, ", "))
	}

	// Warnings the CLI would print when loading this manifest.
	var warnings []string
	for _, name := range in.missing {
		warnings = append(warnings, fmt.Sprintf("environment variable %s is not set; replaced with an empty string", name))
	}
	warnings = append(warnings, cfg.DeprecationWarnings()...)
	if len(warnings) > 0 {
		b.WriteString("\nWarnings:\n")
		for _, w := range warnings {
			b.WriteString("- " + w + "\n")
		}
	}
	return b.String()
}

func sopsSummary(s *manifest.SopsConfig) string {
	switch {
	case s == nil:
		return "not configured"
	case s.Age != nil && s.Pgp != nil:
		return "age and pgp"
	case s.Age != nil:
		if s.Age.KeyFile != "" {
			return "age, key file `" + s.Age.KeyFile + "`"
		}
		return "age"
	case s.Pgp != nil:
		return "pgp"
	}
	return "not configured"
}

func reachedThrough(name string, cc manifest.ContextConfig, endpoints map[string]string) string {
	if cc.Host != "" {
		return "host `" + cc.Host + "`"
	}
	if ep := endpoints[name]; ep != "" {
		return "docker context `" + name + "` (" + ep + ")"
	}
	return "docker context `" + name + "`"
}

// keptList names the declared resources, marking the ones destroy leaves alone.
func keptList[T any](m map[string]T, kept func(T) bool) string {
	var out []string
	for _, name := range sortedKeys(m) {
		if kept(m[name]) {
			name += " (destroy: false)"
		}
		out = append(out, name)
	}
	return orDash(out)
}

func contextSecretsPath(cfg *manifest.Config, context string) string {
	p := filepath.Join(cfg.BaseDir, context, cfg.Discovery.GetSecretsFile())
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}

// stackSecrets lists a stack's own secrets files relative to its root,
// leaving out the context-wide file shown separately.
func stackSecrets(cfg *manifest.Config, s manifest.Stack) []string {
	shared := contextSecretsPath(cfg, s.Context)
	var out []string
	for _, p := range s.SopsSecrets {
		if p == shared {
			continue
		}
		out = append(out, rel(s.Root, p))
	}
	return out
}

func envSummary(s manifest.Stack) string {
	parts := append([]string(nil), s.EnvFile...)
	if n := len(s.EnvInline); n > 0 {
		parts = append(parts, fmt.Sprintf("%d inline", n))
	}
	return orDash(parts)
}

func rel(base, p string) string {
	if base == "" || !filepath.IsAbs(p) {
		return p
	}
	if r, err := filepath.Rel(base, p); err == nil {
		return r
	}
	return p
}

func orDash(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ", ")
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
