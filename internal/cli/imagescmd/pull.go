package imagescmd

import (
	"context"
	"fmt"
	"sort"

	"github.com/charmbracelet/lipgloss"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/images"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/registry"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

func newPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull [service...]",
		Short: "Pull images whose remote digest has changed (same tag, new content)",
		Long: `Pull images where the remote digest differs from the local copy.

This updates images on the remote Docker daemon without modifying compose files.
Use --recreate to also restart affected containers so they run the new image.
Because that restarts running services, --recreate lists them and asks for
confirmation first; pass --skip-confirmation to skip the prompt.

With no positional arguments, every service in scope is considered. Pass
service names to narrow the pull; combine with --stack to scope those names to
a single stack. A typo or unmatched name fails with an error listing the
services available in scope.`,
		Args: cobra.ArbitraryArgs,
		RunE: runPull,
	}

	cmd.Flags().Bool("recreate", false, "Recreate containers after pulling to apply the new image")
	cmd.Flags().Bool("dry-run", false, "Show what would be pulled without making any changes")
	cmd.Flags().Bool("skip-confirmation", false, "With --recreate, skip the confirmation prompt and recreate immediately")

	common.AddTargetFlags(cmd)

	return cmd
}

func runPull(cmd *cobra.Command, args []string) error {
	// Same setup as plan/apply: SSH transport, target flags, reachability.
	clictx, _, err := common.ConnectContexts(cmd)
	if err != nil {
		return err
	}
	pr := clictx.Printer.(ui.StdPrinter)
	cfg, factory := clictx.Config, clictx.Factory

	reg := registry.NewOCIClient(nil)

	inputs, err := buildCheckInputs(cmd.Context(), cfg, factoryClientGetter(factory, cfg))
	if err != nil {
		return err
	}

	if len(inputs) == 0 {
		pr.Plain("No stacks with images found.")
		return nil
	}

	inputs, err = filterInputsByServices(inputs, args)
	if err != nil {
		return err
	}

	var results []images.ImageStatus
	err = common.SpinnerOperation(pr, "Checking images...", func() error {
		localDigests := prefetchLocalDigests(cmd.Context(), inputs, makeLocalDigestFunc(cfg, factory, projectsByStack(inputs)))
		results, err = images.Check(cmd.Context(), inputs, reg, func(_ context.Context, stackKey, service, _ string) (string, error) {
			return localDigests[stackKey+"|"+service], nil
		})
		return err
	})
	if err != nil {
		return err
	}

	// Only care about same-tag digest drift: DigestStale, no newer tags, no error.
	var stale []images.ImageStatus
	for _, r := range results {
		if r.DigestStale && len(r.NewerTags) == 0 && r.Error == "" {
			stale = append(stale, r)
		}
	}

	if len(stale) == 0 {
		pr.Plain("%s  All images are current, no digest drift detected.", ui.GreenText("✓"))
		return nil
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	recreate, _ := cmd.Flags().GetBool("recreate")
	skipConfirm, _ := cmd.Flags().GetBool("skip-confirmation")

	if dryRun {
		renderPullPreview(pr, stale, recreate, true)
		return nil
	}

	allStacks := cfg.GetAllStacks()

	return confirmAndPull(cmd, pr, stale, recreate, skipConfirm, func() error {
		err := common.SpinnerOperation(pr, "Pulling images...", func() error {
			return executePull(cmd.Context(), stale, allStacks, factoryClientGetter(factory, cfg), cfg, recreate)
		})
		if err != nil {
			return err
		}
		renderPullTerminal(pr, stale, recreate)
		return nil
	})
}

// confirmAndPull runs pull for the stale images. With --recreate it first
// lists every service that will be recreated and asks, like apply: restarting
// running containers cannot be undone. A plain pull only downloads images and
// never asks.
func confirmAndPull(cmd *cobra.Command, pr ui.StdPrinter, stale []images.ImageStatus, recreate, skipConfirm bool, pull func() error) error {
	if recreate {
		renderPullPreview(pr, stale, true, false)
		confirmed, err := common.GetConfirmation(cmd, pr, common.ConfirmationOptions{
			SkipConfirmation: skipConfirm,
			Message:          "│ Dockform will pull these images and recreate the services listed above.\n│ Type yes to confirm.\n│",
		})
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}
	return pull()
}

// stackPullGroup aggregates stale images that belong to the same stack.
type stackPullGroup struct {
	stackKey string
	stack    manifest.Stack
	services []string
	statuses []images.ImageStatus
}

func groupByStackForPull(stale []images.ImageStatus, allStacks map[string]manifest.Stack) []stackPullGroup {
	index := make(map[string]*stackPullGroup)
	var order []string

	for _, r := range stale {
		if _, ok := index[r.Stack]; !ok {
			stack := allStacks[r.Stack]
			index[r.Stack] = &stackPullGroup{
				stackKey: r.Stack,
				stack:    stack,
			}
			order = append(order, r.Stack)
		}
		g := index[r.Stack]
		g.services = append(g.services, r.Service)
		g.statuses = append(g.statuses, r)
	}

	sort.Strings(order)
	groups := make([]stackPullGroup, 0, len(order))
	for _, k := range order {
		groups = append(groups, *index[k])
	}
	return groups
}

func executePull(ctx context.Context, stale []images.ImageStatus, allStacks map[string]manifest.Stack, getClient clientGetter, cfg *manifest.Config, recreate bool) error {
	groups := groupByStackForPull(stale, allStacks)

	for _, g := range groups {
		ctxName, _, err := manifest.ParseStackKey(g.stackKey)
		if err != nil {
			return err
		}
		client := getClient(ctxName)

		projName := ""
		if g.stack.Project != nil {
			projName = g.stack.Project.Name
		}

		inline, err := stackInlineEnv(ctx, g.stack, cfg)
		if err != nil {
			return err
		}

		if _, err := client.ComposePull(ctx, g.stack.RootAbs, g.stack.Files, g.stack.Profiles, g.stack.EnvFile, projName, g.services, inline); err != nil {
			return err
		}

		if recreate {
			if _, err := client.ComposeUp(ctx, g.stack.RootAbs, g.stack.Files, g.stack.Profiles, g.stack.EnvFile, projName, inline); err != nil {
				return err
			}
		}
	}

	return nil
}

// renderPullPreview lists each stale image under its stack, with the service
// that uses it: the images a pull would download and, with --recreate, the
// services it would restart.
func renderPullPreview(pr ui.Printer, stale []images.ImageStatus, recreate, dryRun bool) {
	heading := fmt.Sprintf("%d image(s) with digest drift", len(stale))
	if dryRun {
		heading += " (dry run)"
	}
	pr.Plain("%s  %s\n", ui.YellowText("⚠"), heading)

	boldStyle := lipgloss.NewStyle().Bold(true)
	lastStack := ""

	for _, r := range stale {
		if r.Stack != lastStack {
			if lastStack != "" {
				pr.Plain("")
			}
			pr.Plain("%s", boldStyle.Render(r.Stack))
			lastStack = r.Stack
		}
		pr.Plain("  %s  %s: %s", ui.YellowText("→"), r.Service, r.Image)
	}

	switch {
	case recreate && dryRun:
		pr.Plain("\nThe services listed above would be recreated to run the new images.")
	case recreate:
		pr.Plain("\nThe services listed above will be recreated to run the new images.")
	default:
		pr.Plain("\nPass --recreate to restart containers with the new images.")
	}
}

// renderPullTerminal reports a finished pull. After --recreate the preview
// already listed every image and service, so only the summary line is printed.
func renderPullTerminal(pr ui.Printer, stale []images.ImageStatus, recreate bool) {
	if recreate {
		pr.Plain("%s  %d image(s) pulled and containers recreated.", ui.GreenText("✓"), len(stale))
		return
	}

	boldStyle := lipgloss.NewStyle().Bold(true)
	lastStack := ""

	for _, r := range stale {
		if r.Stack != lastStack {
			if lastStack != "" {
				pr.Plain("")
			}
			pr.Plain("%s", boldStyle.Render(r.Stack))
			lastStack = r.Stack
		}
		pr.Plain("  %s  %s", ui.GreenText("✓"), r.Image)
	}

	pr.Plain("")
	pr.Plain("%s  %d image(s) pulled.", ui.GreenText("✓"), len(stale))
	pr.Plain("%s  Pass --recreate to restart containers with the new images.", ui.YellowText("→"))
}
