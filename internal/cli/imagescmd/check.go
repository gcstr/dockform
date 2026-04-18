package imagescmd

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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

// tagPatternLabel is the Docker Compose service label that configures tag
// matching for `dockform images check` / `upgrade`. When absent, the service
// is only checked for digest drift.
const tagPatternLabel = "dockform.tag_pattern"

func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check image freshness across compose stacks",
		RunE:  runCheck,
	}

	cmd.Flags().Bool("json", false, "Output results as JSON")
	cmd.Flags().Bool("all", false, "Show all images, including those that are up to date")
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

	// Run the check inside the spinner so the user sees feedback immediately.
	// Local digest pre-fetching (sequential SSH calls) and remote registry checks
	// (parallel HTTPS calls) both happen here.
	var results []images.ImageStatus
	err = common.SpinnerOperation(pr, "Checking images...", func() error {
		// Pre-fetch local digests sequentially — exec.Command over SSH contexts is
		// unreliable when concurrent, so this must stay sequential.
		localDigests := prefetchLocalDigests(cmd.Context(), inputs, makeLocalDigestFunc(cfg, factory))

		results, err = images.Check(cmd.Context(), inputs, reg, func(_ context.Context, stackKey, service, _ string) (string, error) {
			return localDigests[stackKey+"|"+service], nil
		})
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

	showAll, _ := cmd.Flags().GetBool("all")
	renderTerminal(pr, results, showAll)
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

		services := make(map[string]images.ServiceSpec, len(doc.Services))
		for svcName, svc := range doc.Services {
			if svc.Image == "" {
				continue
			}
			services[svcName] = images.ServiceSpec{
				Image:      svc.Image,
				TagPattern: svc.Labels[tagPatternLabel],
			}
		}

		if len(services) == 0 {
			continue
		}

		inputs = append(inputs, images.CheckInput{
			StackKey: stackKey,
			Services: services,
		})
	}

	return inputs, nil
}

// makeLocalDigestFunc creates a LocalDigestFunc that returns the repo digest of
// the image currently running inside the container for a given service, falling
// back to the stored image digest when no container is found.
//
// Comparing the container's digest (rather than the stored image's digest)
// ensures that a service whose image has been pulled but not yet recreated
// still appears stale — the container is still running the old image.
//
// Performance: two calls are issued per Docker context (daemon), regardless of
// how many services or stacks share that context:
//   1. docker ps   — maps every running compose container to its image ID
//   2. docker image inspect (batched) — maps each image ID to its repo digest
//
// Everything is cached in the closure; calls must be sequential (see
// prefetchLocalDigests). Failures are best-effort: an empty digest makes the
// image appear stale, which is safe.
func makeLocalDigestFunc(cfg *manifest.Config, factory *dockercli.DefaultClientFactory) images.LocalDigestFunc {
	type ctxCache struct {
		containerImageID map[string]string // "project|service" → full image ID
		imageDigest      map[string]string // full image ID → repo digest (sha256:…)
	}
	cache := make(map[string]*ctxCache) // contextName → populated on first use

	return func(ctx context.Context, stackKey, service, imageRef string) (string, error) {
		ctxName, _, err := manifest.ParseStackKey(stackKey)
		if err != nil {
			return "", nil //nolint:nilerr // best-effort
		}
		client := factory.GetClientForContext(ctxName, cfg)

		// Populate cache for this context on first access.
		cc, ok := cache[ctxName]
		if !ok {
			cc = &ctxCache{
				containerImageID: make(map[string]string),
				imageDigest:      make(map[string]string),
			}

			// One docker ps call for all compose containers on this daemon.
			containerMap, _ := client.ComposeContainerImageMap(ctx) //nolint:nilerr // best-effort
			if containerMap != nil {
				cc.containerImageID = containerMap

				// Collect unique image IDs, then batch-fetch their repo digests.
				seen := make(map[string]struct{}, len(containerMap))
				imageIDs := make([]string, 0, len(containerMap))
				for _, id := range containerMap {
					if id == "" {
						continue
					}
					if _, exists := seen[id]; !exists {
						seen[id] = struct{}{}
						imageIDs = append(imageIDs, id)
					}
				}
				if len(imageIDs) > 0 {
					digestMap, _ := client.ImageRepoDigestMap(ctx, imageIDs) //nolint:nilerr // best-effort
					if digestMap != nil {
						cc.imageDigest = digestMap
					}
				}
			}
			cache[ctxName] = cc
		}

		// Look up the running container's digest for this (stack, service).
		allStacks := cfg.GetAllStacks()
		stack := allStacks[stackKey]
		proj := effectiveProjectName(stack)

		if imageID := cc.containerImageID[proj+"|"+service]; imageID != "" {
			if digest := cc.imageDigest[imageID]; digest != "" {
				return digest, nil
			}
		}

		// Fallback: stored image digest (for services with no running container).
		out, err := client.ImageInspectRepoDigests(ctx, imageRef)
		if err != nil || len(out) == 0 {
			return "", nil //nolint:nilerr // best-effort
		}
		for _, rd := range out {
			if idx := strings.LastIndex(rd, "@"); idx >= 0 {
				return rd[idx+1:], nil
			}
		}
		return "", nil
	}
}

// effectiveProjectName returns the Docker Compose project name for a stack.
// When no explicit override is set, Compose defaults to the lowercase basename
// of the working directory.
func effectiveProjectName(stack manifest.Stack) string {
	if stack.Project != nil && stack.Project.Name != "" {
		return strings.ToLower(stack.Project.Name)
	}
	return strings.ToLower(filepath.Base(stack.RootAbs))
}

// prefetchLocalDigests calls localDigestFn sequentially for every (stack, service)
// pair across all inputs and returns a map keyed by "stackKey|service".
// Keyed by service (not image ref) because different stacks may use different
// Docker daemons, and the local digest now reflects the running container rather
// than the stored image. Running exec.Command concurrently (especially over SSH
// contexts) is unreliable, hence sequential.
func prefetchLocalDigests(ctx context.Context, inputs []images.CheckInput, fn images.LocalDigestFunc) map[string]string {
	out := make(map[string]string)
	for _, input := range inputs {
		for svcName, spec := range input.Services {
			key := input.StackKey + "|" + svcName
			if _, seen := out[key]; seen {
				continue
			}
			digest, _ := fn(ctx, input.StackKey, svcName, spec.Image) // best-effort: empty string on failure
			out[key] = digest
		}
	}
	return out
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

func renderTerminal(pr ui.Printer, results []images.ImageStatus, showAll bool) {
	if len(results) == 0 {
		pr.Plain("\nNo images found.")
		return
	}

	pr.Plain("")

	// Split into attention-needed and ok.
	var attention, ok []images.ImageStatus
	for _, r := range results {
		if r.Error != "" || r.DigestStale || len(r.NewerTags) > 0 {
			attention = append(attention, r)
		} else {
			ok = append(ok, r)
		}
	}

	dimStyle := lipgloss.NewStyle().Faint(true)
	headerStyle := lipgloss.NewStyle().Faint(true).Bold(true)

	// computeBaseWidths computes widths for STACK, IMAGE, TAG given rows.
	computeBaseWidths := func(rows []images.ImageStatus) (int, int, int) {
		wStack, wImage, wTag := len("STACK"), len("IMAGE"), len("TAG")
		for _, r := range rows {
			stack, image, tag := r.Stack, imageNameWithoutTag(r.Image), r.CurrentTag
			if len(stack) > wStack {
				wStack = len(stack)
			}
			if len(image) > wImage {
				wImage = len(image)
			}
			if len(tag) > wTag {
				wTag = len(tag)
			}
		}
		return wStack, wImage, wTag
	}

	// upgradeCell returns the raw text (without styling) for the UPGRADE column.
	upgradeCellRaw := func(r images.ImageStatus) string {
		if len(r.NewerTags) > 0 {
			return r.NewerTags[0]
		}
		if !r.HasTagPattern {
			return "unknown"
		}
		return "-"
	}

	// upgradeCellStyled returns the styled UPGRADE cell for rendering.
	upgradeCellStyled := func(r images.ImageStatus, width int) string {
		raw := upgradeCellRaw(r)
		padded := fmt.Sprintf("%-*s", width, raw)
		if len(r.NewerTags) > 0 {
			return ui.YellowText(padded)
		}
		if !r.HasTagPattern {
			return dimStyle.Render(padded)
		}
		return padded
	}

	renderAttentionTable := func(rows []images.ImageStatus) {
		wStack, wImage, wTag := computeBaseWidths(rows)
		wUpgrade := len("UPGRADE")
		for _, r := range rows {
			if r.Error != "" {
				continue
			}
			if l := len(upgradeCellRaw(r)); l > wUpgrade {
				wUpgrade = l
			}
		}
		// Header.
		pr.Plain("  %s  %s  %s  %s  %s",
			headerStyle.Render(fmt.Sprintf("%-*s", wStack, "STACK")),
			headerStyle.Render(fmt.Sprintf("%-*s", wImage, "IMAGE")),
			headerStyle.Render(fmt.Sprintf("%-*s", wTag, "TAG")),
			headerStyle.Render(fmt.Sprintf("%-*s", wUpgrade, "UPGRADE")),
			headerStyle.Render("DIGEST"),
		)
		for _, r := range rows {
			stack := fmt.Sprintf("%-*s", wStack, r.Stack)
			image := fmt.Sprintf("%-*s", wImage, imageNameWithoutTag(r.Image))
			tag := fmt.Sprintf("%-*s", wTag, r.CurrentTag)

			if r.Error != "" {
				pr.Plain("  %s  %s  %s  %s", stack, image, tag, ui.YellowText("! "+r.Error))
				continue
			}

			upgrade := upgradeCellStyled(r, wUpgrade)
			var digest string
			if r.DigestStale {
				digest = ui.YellowText("changed")
			} else {
				digest = "-"
			}
			pr.Plain("  %s  %s  %s  %s  %s", stack, image, tag, upgrade, digest)
		}
	}

	renderOkTable := func(rows []images.ImageStatus) {
		wStack, wImage, wTag := computeBaseWidths(rows)
		pr.Plain("  %s  %s  %s  %s",
			headerStyle.Render(fmt.Sprintf("%-*s", wStack, "STACK")),
			headerStyle.Render(fmt.Sprintf("%-*s", wImage, "IMAGE")),
			headerStyle.Render(fmt.Sprintf("%-*s", wTag, "TAG")),
			headerStyle.Render("STATUS"),
		)
		for _, r := range rows {
			stack := fmt.Sprintf("%-*s", wStack, r.Stack)
			image := fmt.Sprintf("%-*s", wImage, imageNameWithoutTag(r.Image))
			tag := fmt.Sprintf("%-*s", wTag, r.CurrentTag)
			status := ui.GreenText("up to date")
			if !r.HasTagPattern {
				status += "  " + dimStyle.Render("no tag_pattern")
			}
			pr.Plain("  %s  %s  %s  %s", stack, image, tag, status)
		}
	}

	if len(attention) > 0 {
		pr.Plain("%s  %d image(s) need attention\n", ui.YellowText("⚠"), len(attention))
		renderAttentionTable(attention)
	}

	if len(ok) > 0 {
		if len(attention) > 0 {
			pr.Plain("")
		}
		if showAll {
			pr.Plain("%s  %d image(s) up to date\n", ui.GreenText("✓"), len(ok))
			renderOkTable(ok)

			hasMissingPattern := false
			for _, r := range ok {
				if !r.HasTagPattern {
					hasMissingPattern = true
					break
				}
			}
			if hasMissingPattern {
				icon := lipgloss.NewStyle().Foreground(lipgloss.Color("69")).Render("ℹ")
				badge := dimStyle.Render(`"no tag_pattern"`)
				labelName := lipgloss.NewStyle().Italic(true).
					Foreground(lipgloss.AdaptiveColor{Light: "#3478F6", Dark: "#4A9EFF"}).
					Render(tagPatternLabel)
				prefix := dimStyle.Render("Rows marked ")
				middle := dimStyle.Render(" are only checked for digest drift. Add a ")
				suffix := dimStyle.Render(" label to the service to track newer tags.")
				pr.Plain("\n%s  %s%s%s%s%s", icon, prefix, badge, middle, labelName, suffix)
			}
		} else {
			pr.Plain("%s  %d image(s) up to date  %s",
				ui.GreenText("✓"), len(ok), dimStyle.Render("(--all to show)"))
		}
	}
}

// imageNameWithoutTag strips the tag from an image reference.
func imageNameWithoutTag(image string) string {
	// Find last slash to isolate the name:tag part.
	lastSlash := strings.LastIndex(image, "/")
	nameTag := image[lastSlash+1:]
	if idx := strings.LastIndex(nameTag, ":"); idx >= 0 {
		return image[:lastSlash+1+idx]
	}
	return image
}

