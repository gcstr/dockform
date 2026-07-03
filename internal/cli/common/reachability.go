package common

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

// ReachabilityProbeTimeout bounds each per-context daemon probe so an unreachable
// host (e.g. a down SSH context) cannot hang the command. Tests override it; it is
// not safe to mutate from parallel (t.Parallel) tests.
var ReachabilityProbeTimeout = 10 * time.Second

// ContextProbeResult is the outcome of probing a single Docker context's daemon.
type ContextProbeResult struct {
	Name  string
	Cause string // empty when reachable
}

// Reachable reports whether the probed context's daemon responded successfully.
func (r ContextProbeResult) Reachable() bool { return r.Cause == "" }

// EnsureContextsReachable probes every context in cfg in parallel and returns an
// aggregated Unavailable error (exit 69) if any daemon is unreachable. cfg is
// expected to already be narrowed to the selected contexts by ResolveTargets.
func EnsureContextsReachable(ctx context.Context, cfg *manifest.Config, factory dockercli.ClientFactory) error {
	results := ProbeContextsReachability(ctx, cfg, factory)
	if len(results) == 0 {
		return nil
	}

	var failed []ContextProbeResult
	for _, r := range results {
		if !r.Reachable() {
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
		fmt.Fprintf(&b, "  • %s: %s\n", r.Name, r.Cause)
	}
	b.WriteString("Check the hosts are up and your Docker contexts are correct (docker context ls).")
	return apperr.New("common.EnsureContextsReachable", apperr.Unavailable, "%s", b.String())
}

// ProbeContextsReachability probes every context in cfg in parallel, each bounded
// by ReachabilityProbeTimeout, and returns one result per context sorted by name.
// Callers that need per-context pass/fail reporting (e.g. `dockform doctor`) should
// use this directly instead of EnsureContextsReachable, which only returns an
// aggregated error.
func ProbeContextsReachability(ctx context.Context, cfg *manifest.Config, factory dockercli.ClientFactory) []ContextProbeResult {
	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)

	results := make([]ContextProbeResult, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			results[i] = ContextProbeResult{Name: name, Cause: probeContext(ctx, name, cfg, factory)}
		}(i, name)
	}
	wg.Wait()
	return results
}

// probeContext returns an empty string when the context's daemon is reachable, or
// a short human-readable cause when it is not.
func probeContext(ctx context.Context, name string, cfg *manifest.Config, factory dockercli.ClientFactory) string {
	probeCtx, cancel := context.WithTimeout(ctx, ReachabilityProbeTimeout)
	defer cancel()

	client := factory.GetClientForContext(name, cfg)
	if err := client.CheckDaemon(probeCtx); err != nil {
		// Parent context cancelled or expired — not our probe timeout; surface as-is.
		if ctx.Err() != nil {
			return err.Error()
		}
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			return fmt.Sprintf("timed out after %s", ReachabilityProbeTimeout)
		}
		return err.Error()
	}
	return ""
}
