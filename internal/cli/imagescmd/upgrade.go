package imagescmd

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gcstr/dockform/internal/cli/common"
	"github.com/gcstr/dockform/internal/images"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/registry"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

func newUpgradeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade image tags in compose files to the newest available versions",
		RunE:  runUpgrade,
	}

	cmd.Flags().Bool("dry-run", false, "Preview changes without writing files")

	common.AddTargetFlags(cmd)

	return cmd
}

func runUpgrade(cmd *cobra.Command, _ []string) error {
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

	// Pre-fetch local digests sequentially before parallel registry checks.
	localDigests := prefetchLocalDigests(cmd.Context(), inputs, makeLocalDigestFunc(cfg, factory))

	// Run the check.
	var results []images.ImageStatus
	err = common.SpinnerOperation(pr, "Checking images...", func() error {
		results, err = images.Check(cmd.Context(), inputs, reg, func(ctx context.Context, stackKey, imageRef string) (string, error) {
			return localDigests[stackKey+"|"+imageRef], nil
		})
		return err
	})
	if err != nil {
		return err
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Build stackFiles map: stack key -> list of absolute compose file paths.
	stackFiles := buildStackFiles(cfg)

	var changes []images.FileChange
	if !dryRun {
		changes, err = images.Upgrade(results, stackFiles)
		if err != nil {
			return err
		}
	}

	renderUpgradeTerminal(pr, results, changes, stackFiles, dryRun)
	return nil
}

// buildStackFiles builds a map of stack key to absolute compose file paths.
func buildStackFiles(cfg *manifest.Config) map[string][]string {
	allStacks := cfg.GetAllStacks()
	stackFiles := make(map[string][]string, len(allStacks))

	for stackKey, stack := range allStacks {
		paths := make([]string, 0, len(stack.Files))
		for _, f := range stack.Files {
			if filepath.IsAbs(f) {
				paths = append(paths, f)
			} else {
				paths = append(paths, filepath.Join(stack.RootAbs, f))
			}
		}
		stackFiles[stackKey] = paths
	}

	return stackFiles
}

func renderUpgradeTerminal(pr ui.Printer, results []images.ImageStatus, changes []images.FileChange, stackFiles map[string][]string, dryRun bool) {
	if len(results) == 0 {
		pr.Plain("\nNo images found.")
		return
	}

	// Build a quick lookup: (stack, service) -> FileChange
	type changeKey struct{ stack, service string }
	changeMap := make(map[changeKey]images.FileChange, len(changes))
	for _, c := range changes {
		changeMap[changeKey{c.StackKey, c.Service}] = c
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

	// Sort groups for deterministic output.
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].key < groups[j].key
	})

	boldStyle := lipgloss.NewStyle().Bold(true)
	for i, g := range groups {
		if i > 0 {
			pr.Plain("")
		}
		pr.Plain("%s", boldStyle.Render(g.key))

		for _, r := range g.results {
			imageRef := r.Image

			if r.Error != "" {
				pr.Plain("  %-40s %s %s", imageRef, ui.YellowText("⚠"), r.Error)
				continue
			}

			if len(r.NewerTags) > 0 {
				newTag := r.NewerTags[0]

				// Derive image name without tag.
				imageName := r.Image
				if idx := strings.LastIndex(r.Image, ":"); idx != -1 {
					imageName = r.Image[:idx]
				}

				newRef := imageName + ":" + newTag

				ck := changeKey{r.Stack, r.Service}
				if fc, ok := changeMap[ck]; ok {
					// File was updated (real run).
					composeFile := filepath.Base(fc.File)
					pr.Plain("  %s → %s   (%s updated)", imageRef, newRef, composeFile)
				} else if dryRun {
					// Check if it would be found in compose files.
					files := stackFiles[r.Stack]
					if len(files) > 0 {
						pr.Plain("  %s → %s   (dry run)", imageRef, newRef)
					} else {
						pr.Plain("  %-40s %s tag not found in compose file", imageRef+" → "+newRef, ui.YellowText("⚠"))
					}
				} else {
					// Upgrade ran but image wasn't found in files.
					pr.Plain("  %-40s %s tag not found in compose file", imageRef+" → "+newRef, ui.YellowText("⚠"))
				}
				continue
			}

			if r.DigestStale && len(r.NewerTags) == 0 && r.Error == "" {
				pr.Plain("  %-40s %s no tag_pattern configured — run `docker compose pull`", imageRef, ui.YellowText("⚠"))
				continue
			}

			pr.Plain("  %-40s %s already latest", imageRef, ui.GreenText("✓"))
		}
	}
}
