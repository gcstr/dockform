package manifest

import (
	"fmt"
	"sort"
	"strings"
)

// DeprecationWarnings lists manifest settings that still work but are on their
// way out, one message per occurrence, sorted for stable output.
func (c Config) DeprecationWarnings() []string {
	var out []string
	for key, stack := range c.Stacks {
		// secrets.sops on a discovered stack adds files next to the secrets
		// file discovery already loads from the stack directory. Explicit
		// stacks (root:) have no discovered file, so there it stays the way
		// to load secrets.
		if _, discovered := c.DiscoveredStacks[key]; discovered && stack.Secrets != nil && len(stack.Secrets.Sops) > 0 {
			out = append(out, fmt.Sprintf(
				"stack %s: secrets.sops is deprecated for discovered stacks and will be removed in a future release; move the values in %s into the stack's %s",
				key, strings.Join(stack.Secrets.Sops, ", "), c.Discovery.GetSecretsFile()))
		}
	}
	sort.Strings(out)
	return out
}
