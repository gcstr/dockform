package imagescmd

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/images"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/registry"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check image freshness across compose stacks",
		RunE:  runCheck,
	}

	cmd.Flags().Bool("json", false, "Output results as JSON")
	cmd.Flags().Bool("sequential", false, "Disable parallel checks (reserved for future use)")

	common.AddTargetFlags(cmd)

	return cmd
}

func runCheck(cmd *cobra.Command, _ []string) error {
	pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}

	// Load configuration with warnings.
	cfg, err := common.LoadConfigWithWarnings(cmd, pr)
	if err != nil {
		return err
	}

	// Apply target filtering if flags are provided.
	opts := common.ReadTargetOptions(cmd)
	if !opts.IsEmpty() {
		cfg, err = common.ResolveTargets(cfg, opts)
		if err != nil {
			return err
		}
	}

	common.DisplayDaemonInfo(pr, cfg)

	// Create client factory for multi-context support.
	factory := common.CreateClientFactory()

	// Create registry client.
	reg := registry.NewOCIClient(nil)

	// Build check inputs from all stacks.
	inputs, err := buildCheckInputs(cmd.Context(), cfg, factory)
	if err != nil {
		return err
	}

	if len(inputs) == 0 {
		pr.Plain("\nNo stacks with images found.")
		return nil
	}

	// Build a local digest function using the factory.
	localDigestFn := makeLocalDigestFunc(cfg, factory)

	// Run the check.
	var results []images.ImageStatus
	err = common.SpinnerOperation(pr, "Checking images...", func() error {
		results, err = images.Check(cmd.Context(), inputs, reg, localDigestFn)
		return err
	})
	if err != nil {
		return err
	}

	// Render output.
	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		return renderJSON(cmd, results)
	}

	renderTerminal(pr, results)
	return nil
}

// buildCheckInputs iterates over all stacks and builds CheckInput entries
// by calling ComposeConfigFull to discover service images.
func buildCheckInputs(ctx context.Context, cfg *manifest.Config, factory *dockercli.DefaultClientFactory) ([]images.CheckInput, error) {
	allStacks := cfg.GetAllStacks()

	// Sort stack keys for deterministic output.
	stackKeys := make([]string, 0, len(allStacks))
	for k := range allStacks {
		stackKeys = append(stackKeys, k)
	}
	sort.Strings(stackKeys)

	var inputs []images.CheckInput

	for _, stackKey := range stackKeys {
		stack := allStacks[stackKey]

		ctxName, _, err := manifest.ParseStackKey(stackKey)
		if err != nil {
			return nil, err
		}

		// Get a docker client for this stack's context.
		client := factory.GetClientForContext(ctxName, cfg)

		// Get the full compose config to extract service images.
		doc, err := client.ComposeConfigFull(ctx, stack.RootAbs, stack.Files, stack.Profiles, stack.EnvFile, stack.EnvInline)
		if err != nil {
			return nil, apperr.Wrap("imagescmd.buildCheckInputs", apperr.External, err, "failed to get compose config for stack %s", stackKey)
		}

		if len(doc.Services) == 0 {
			continue
		}

		services := make(map[string]string, len(doc.Services))
		for svcName, svc := range doc.Services {
			if svc.Image != "" {
				services[svcName] = svc.Image
			}
		}

		if len(services) == 0 {
			continue
		}

		input := images.CheckInput{
			StackKey: stackKey,
			Services: services,
		}

		if stack.Images != nil {
			input.TagPattern = stack.Images.TagPattern
		}

		inputs = append(inputs, input)
	}

	return inputs, nil
}

// makeLocalDigestFunc creates a LocalDigestFunc that uses docker image inspect
// to retrieve the local repo digest for an image. This is best-effort: if the
// image is not pulled or has no repo digests, it returns an empty string (which
// will cause the image to appear stale).
func makeLocalDigestFunc(cfg *manifest.Config, factory *dockercli.DefaultClientFactory) images.LocalDigestFunc {
	// Use the first context's client for local digest lookups.
	// In a remote-context world this inspects images on the remote host.
	firstCtx := cfg.GetFirstContext()
	client := factory.GetClientForContext(firstCtx, cfg)

	return func(ctx context.Context, imageRef string) (string, error) {
		out, err := client.ImageInspectRepoDigests(ctx, imageRef)
		if err != nil {
			// Image not pulled or inspect failed — treat as stale.
			return "", nil //nolint:nilerr // best-effort
		}

		if len(out) == 0 {
			return "", nil
		}

		// Return the first repo digest. The format is "registry/name@sha256:abc...".
		// We return just the digest portion for comparison with the remote digest.
		for _, rd := range out {
			if idx := strings.LastIndex(rd, "@"); idx >= 0 {
				return rd[idx+1:], nil
			}
		}

		return "", nil
	}
}

// jsonResult is the JSON output format for a single image check result.
type jsonResult struct {
	Stack         string   `json:"stack"`
	Service       string   `json:"service"`
	Image         string   `json:"image"`
	CurrentTag    string   `json:"current_tag"`
	DigestChanged bool     `json:"digest_changed"`
	NewerTags     []string `json:"newer_tags,omitempty"`
	Error         string   `json:"error,omitempty"`
}

func renderJSON(cmd *cobra.Command, results []images.ImageStatus) error {
	out := make([]jsonResult, 0, len(results))
	for _, r := range results {
		out = append(out, jsonResult{
			Stack:         r.Stack,
			Service:       r.Service,
			Image:         r.Image,
			CurrentTag:    r.CurrentTag,
			DigestChanged: r.DigestStale,
			NewerTags:     r.NewerTags,
			Error:         r.Error,
		})
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return apperr.Wrap("imagescmd.renderJSON", apperr.Internal, err, "failed to encode JSON output")
	}
	return nil
}

func renderTerminal(pr ui.Printer, results []images.ImageStatus) {
	if len(results) == 0 {
		pr.Plain("\nNo images found.")
		return
	}

	// Group results by stack for display.
	type stackGroup struct {
		key     string
		results []images.ImageStatus
	}

	seen := make(map[string]int)
	var groups []stackGroup

	for _, r := range results {
		idx, ok := seen[r.Stack]
		if !ok {
			idx = len(groups)
			seen[r.Stack] = idx
			groups = append(groups, stackGroup{key: r.Stack})
		}
		groups[idx].results = append(groups[idx].results, r)
	}

	boldStyle := lipgloss.NewStyle().Bold(true)
	for i, g := range groups {
		if i > 0 {
			pr.Plain("")
		}
		pr.Plain("%s", boldStyle.Render(g.key))

		for _, r := range g.results {
			if r.Error != "" {
				pr.Plain("  %-40s %s %s", r.Image, ui.YellowText("⚠"), r.Error)
				continue
			}

			if len(r.NewerTags) > 0 {
				tags := strings.Join(r.NewerTags, ", ")
				pr.Plain("  %-40s %s newer versions: %s", r.Image, ui.YellowText("⚠"), tags)
			} else if r.DigestStale {
				pr.Plain("  %-40s %s updated upstream", r.Image, ui.YellowText("⚠"))
			} else {
				pr.Plain("  %-40s %s up to date", r.Image, ui.GreenText("✓"))
			}
		}
	}
}
