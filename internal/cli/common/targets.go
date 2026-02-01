package common

import (
	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
)

// TargetOptions represents CLI targeting flags for filtering contexts/stacks.
type TargetOptions struct {
	Contexts   []string // --context flag values
	Stacks     []string // --stack flag values (context/stack format)
	Deployment string   // --deployment flag value
}

// IsEmpty returns true if no targeting flags were provided.
func (t TargetOptions) IsEmpty() bool {
	return len(t.Contexts) == 0 && len(t.Stacks) == 0 && t.Deployment == ""
}

// ResolveTargets filters a config to only include the targeted contexts and stacks.
// If no targeting options are set, the config is returned unchanged.
// The returned config is a shallow copy with filtered Contexts, Stacks, and DiscoveredStacks/Filesets.
func ResolveTargets(cfg *manifest.Config, opts TargetOptions) (*manifest.Config, error) {
	if opts.IsEmpty() {
		return cfg, nil
	}

	// Resolve deployment into explicit context/stack lists
	if opts.Deployment != "" {
		deploy, ok := cfg.Deployments[opts.Deployment]
		if !ok {
			return nil, apperr.New("ResolveTargets", apperr.InvalidInput, "unknown deployment %q", opts.Deployment)
		}
		// Merge deployment targets with any explicit flags
		opts.Contexts = append(opts.Contexts, deploy.Contexts...)
		opts.Stacks = append(opts.Stacks, deploy.Stacks...)
		opts.Deployment = "" // consumed
	}

	// Build set of allowed contexts
	allowedContexts := make(map[string]bool)
	for _, c := range opts.Contexts {
		if _, ok := cfg.Contexts[c]; !ok {
			return nil, apperr.New("ResolveTargets", apperr.InvalidInput, "unknown context %q", c)
		}
		allowedContexts[c] = true
	}

	// Build set of allowed stacks (context/stack format)
	allowedStacks := make(map[string]bool)
	for _, s := range opts.Stacks {
		context, _, err := manifest.ParseStackKey(s)
		if err != nil {
			return nil, apperr.Wrap("ResolveTargets", apperr.InvalidInput, err, "invalid stack target")
		}
		if _, ok := cfg.Contexts[context]; !ok {
			return nil, apperr.New("ResolveTargets", apperr.InvalidInput, "stack %q references unknown context %q", s, context)
		}
		allowedStacks[s] = true
		// Also allow the context so its config is included
		allowedContexts[context] = true
	}

	// If only --context was provided (no --stack), allow all stacks in those contexts
	contextOnly := len(opts.Stacks) == 0

	filtered := *cfg
	filtered.Contexts = make(map[string]manifest.ContextConfig)
	filtered.Stacks = make(map[string]manifest.Stack)
	filtered.DiscoveredStacks = make(map[string]manifest.Stack)
	filtered.DiscoveredFilesets = make(map[string]manifest.FilesetSpec)

	// Copy allowed contexts
	for name, context := range cfg.Contexts {
		if allowedContexts[name] {
			filtered.Contexts[name] = context
		}
	}

	stackAllowed := func(key string) bool {
		if allowedStacks[key] {
			return true
		}
		if contextOnly {
			context, _, err := manifest.ParseStackKey(key)
			if err != nil {
				return false
			}
			return allowedContexts[context]
		}
		return false
	}

	// Filter explicit stacks
	for key, stack := range cfg.Stacks {
		if stackAllowed(key) {
			filtered.Stacks[key] = stack
		}
	}

	// Filter discovered stacks
	for key, stack := range cfg.DiscoveredStacks {
		if stackAllowed(key) {
			filtered.DiscoveredStacks[key] = stack
		}
	}

	// Filter discovered filesets
	for key, fileset := range cfg.DiscoveredFilesets {
		if allowedContexts[fileset.Context] {
			if contextOnly || stackAllowed(manifest.MakeStackKey(fileset.Context, fileset.Stack)) {
				filtered.DiscoveredFilesets[key] = fileset
			}
		}
	}

	return &filtered, nil
}
