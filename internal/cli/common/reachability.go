package common

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

type contextProbeResult struct {
	name  string
	cause string // empty when reachable
}

// EnsureContextsReachable probes every context in cfg in parallel and returns an
// aggregated Unavailable error (exit 69) if any daemon is unreachable. cfg is
// expected to already be narrowed to the selected contexts by ResolveTargets.
func EnsureContextsReachable(ctx context.Context, cfg *manifest.Config, factory dockercli.ClientFactory) error {
	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)

	results := make([]contextProbeResult, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			results[i] = contextProbeResult{name: name, cause: probeContext(ctx, name, cfg, factory)}
		}(i, name)
	}
	wg.Wait()

	var failed []contextProbeResult
	for _, r := range results {
		if r.cause != "" {
			failed = append(failed, r)
		}
	}
	if len(failed) == 0 {
		return nil
	}

	var b strings.Builder
	if len(failed) == 1 {
		b.WriteString("1 context is unreachable:\n")
	} else {
		fmt.Fprintf(&b, "%d contexts are unreachable:\n", len(failed))
	}
	for _, r := range failed {
		fmt.Fprintf(&b, "  • %s: %s\n", r.name, r.cause)
	}
	b.WriteString("Check the hosts are up and your Docker contexts are correct (docker context ls).")
	return apperr.New("common.EnsureContextsReachable", apperr.Unavailable, "%s", b.String())
}

// probeContext returns an empty string when the context's daemon is reachable, or
// a short human-readable cause when it is not.
func probeContext(ctx context.Context, name string, cfg *manifest.Config, factory dockercli.ClientFactory) string {
	client := factory.GetClientForContext(name, cfg)
	if err := client.CheckDaemon(ctx); err != nil {
		return err.Error()
	}
	return ""
}
