package imagescmd

import (
	"context"
	"sort"

	"github.com/charmbracelet/lipgloss"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/images"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/registry"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

func newPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull images whose remote digest has changed (same tag, new content)",
		Long: `Pull images where the remote digest differs from the local copy.

This updates images on the remote Docker daemon without modifying compose files.
Use --recreate to also restart affected containers so they run the new image.`,
		RunE: runPull,
	}

	cmd.Flags().Bool("recreate", false, "Recreate containers after pulling to apply the new image")
	cmd.Flags().Bool("dry-run", false, "Show what would be pulled without making any changes")

	common.AddTargetFlags(cmd)

	return cmd
}

func runPull(cmd *cobra.Command, _ []string) error {
	pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}

	cfg, err := common.LoadConfigWithWarnings(cmd, pr)
	if err != nil {
		return err
	}

	opts := common.ReadTargetOptions(cmd)
	if !opts.IsEmpty() {
		cfg, err = common.ResolveTargets(cfg, opts)
		if err != nil {
			return err
		}
	}

	common.DisplayDaemonInfo(pr, cfg)

	factory := common.CreateClientFactory()
	reg := registry.NewOCIClient(nil)

	inputs, err := buildCheckInputs(cmd.Context(), cfg, factory)
	if err != nil {
		return err
	}

	if len(inputs) == 0 {
		pr.Plain("\nNo stacks with images found.")
		return nil
	}

	var results []images.ImageStatus
	err = common.SpinnerOperation(pr, "Checking images...", func() error {
		localDigests := prefetchLocalDigests(cmd.Context(), inputs, makeLocalDigestFunc(cfg, factory))
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
		pr.Plain("\n%s  All images are current — no digest drift detected.", ui.GreenText("✓"))
		return nil
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	recreate, _ := cmd.Flags().GetBool("recreate")

	if dryRun {
		renderPullDryRun(pr, stale, recreate)
		return nil
	}

	allStacks := cfg.GetAllStacks()

	err = common.SpinnerOperation(pr, "Pulling images...", func() error {
		return executePull(cmd.Context(), stale, allStacks, factory, cfg, recreate)
	})
	if err != nil {
		return err
	}

	renderPullTerminal(pr, stale, recreate)
	return nil
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

func executePull(ctx context.Context, stale []images.ImageStatus, allStacks map[string]manifest.Stack, factory *dockercli.DefaultClientFactory, cfg *manifest.Config, recreate bool) error {
	groups := groupByStackForPull(stale, allStacks)

	for _, g := range groups {
		ctxName, _, err := manifest.ParseStackKey(g.stackKey)
		if err != nil {
			return err
		}
		client := factory.GetClientForContext(ctxName, cfg)

		projName := ""
		if g.stack.Project != nil {
			projName = g.stack.Project.Name
		}

		if _, err := client.ComposePull(ctx, g.stack.RootAbs, g.stack.Files, g.stack.Profiles, g.stack.EnvFile, projName, g.services, g.stack.EnvInline); err != nil {
			return err
		}

		if recreate {
			if _, err := client.ComposeUp(ctx, g.stack.RootAbs, g.stack.Files, g.stack.Profiles, g.stack.EnvFile, projName, g.stack.EnvInline); err != nil {
				return err
			}
		}
	}

	return nil
}

func renderPullDryRun(pr ui.Printer, stale []images.ImageStatus, recreate bool) {
	pr.Plain("\n%s  %d image(s) with digest drift (dry run)\n", ui.YellowText("⚠"), len(stale))

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
		pr.Plain("  %s  %s", ui.YellowText("→"), r.Image)
	}

	if recreate {
		pr.Plain("\nContainers would be recreated after pull.")
	} else {
		pr.Plain("\nPass --recreate to restart containers with the new images.")
	}
}

func renderPullTerminal(pr ui.Printer, stale []images.ImageStatus, recreate bool) {
	pr.Plain("")

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
	if recreate {
		pr.Plain("%s  %d image(s) pulled and containers recreated.", ui.GreenText("✓"), len(stale))
	} else {
		pr.Plain("%s  %d image(s) pulled.", ui.GreenText("✓"), len(stale))
		pr.Plain("%s  Pass --recreate to restart containers with the new images.", ui.YellowText("→"))
	}
}
