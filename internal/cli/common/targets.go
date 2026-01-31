package common

import (
	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
)

// TargetOptions represents CLI targeting flags for filtering daemons/stacks.
type TargetOptions struct {
	Daemons    []string // --daemon flag values
	Stacks     []string // --stack flag values (daemon/stack format)
	Deployment string   // --deployment flag value
}

// IsEmpty returns true if no targeting flags were provided.
func (t TargetOptions) IsEmpty() bool {
	return len(t.Daemons) == 0 && len(t.Stacks) == 0 && t.Deployment == ""
}

// ResolveTargets filters a config to only include the targeted daemons and stacks.
// If no targeting options are set, the config is returned unchanged.
// The returned config is a shallow copy with filtered Daemons, Stacks, and DiscoveredStacks/Filesets.
func ResolveTargets(cfg *manifest.Config, opts TargetOptions) (*manifest.Config, error) {
	if opts.IsEmpty() {
		return cfg, nil
	}

	// Resolve deployment into explicit daemon/stack lists
	if opts.Deployment != "" {
		deploy, ok := cfg.Deployments[opts.Deployment]
		if !ok {
			return nil, apperr.New("ResolveTargets", apperr.InvalidInput, "unknown deployment %q", opts.Deployment)
		}
		// Merge deployment targets with any explicit flags
		opts.Daemons = append(opts.Daemons, deploy.Daemons...)
		opts.Stacks = append(opts.Stacks, deploy.Stacks...)
		opts.Deployment = "" // consumed
	}

	// Build set of allowed daemons
	allowedDaemons := make(map[string]bool)
	for _, d := range opts.Daemons {
		if _, ok := cfg.Daemons[d]; !ok {
			return nil, apperr.New("ResolveTargets", apperr.InvalidInput, "unknown daemon %q", d)
		}
		allowedDaemons[d] = true
	}

	// Build set of allowed stacks (daemon/stack format)
	allowedStacks := make(map[string]bool)
	for _, s := range opts.Stacks {
		daemon, _, err := manifest.ParseStackKey(s)
		if err != nil {
			return nil, apperr.Wrap("ResolveTargets", apperr.InvalidInput, err, "invalid stack target")
		}
		if _, ok := cfg.Daemons[daemon]; !ok {
			return nil, apperr.New("ResolveTargets", apperr.InvalidInput, "stack %q references unknown daemon %q", s, daemon)
		}
		allowedStacks[s] = true
		// Also allow the daemon so its config is included
		allowedDaemons[daemon] = true
	}

	// If only --daemon was provided (no --stack), allow all stacks in those daemons
	daemonOnly := len(opts.Stacks) == 0

	filtered := *cfg
	filtered.Daemons = make(map[string]manifest.DaemonConfig)
	filtered.Stacks = make(map[string]manifest.Stack)
	filtered.DiscoveredStacks = make(map[string]manifest.Stack)
	filtered.DiscoveredFilesets = make(map[string]manifest.FilesetSpec)

	// Copy allowed daemons
	for name, daemon := range cfg.Daemons {
		if allowedDaemons[name] {
			filtered.Daemons[name] = daemon
		}
	}

	stackAllowed := func(key string) bool {
		if allowedStacks[key] {
			return true
		}
		if daemonOnly {
			daemon, _, err := manifest.ParseStackKey(key)
			if err != nil {
				return false
			}
			return allowedDaemons[daemon]
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
		if allowedDaemons[fileset.Daemon] {
			if daemonOnly || stackAllowed(manifest.MakeStackKey(fileset.Daemon, fileset.Stack)) {
				filtered.DiscoveredFilesets[key] = fileset
			}
		}
	}

	return &filtered, nil
}
