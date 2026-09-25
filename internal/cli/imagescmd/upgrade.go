package imagescmd

import (
	"fmt"
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
		Use:   "upgrade [service...]",
		Short: "Upgrade image tags in compose files to the newest available versions",
		Long: `Upgrade image tags in compose files to the newest available versions.

With no positional arguments, every service in scope is considered. Pass
service names to narrow the upgrade; combine with --stack to scope those names
to a single stack. A typo or unmatched name fails with an error listing the
services available in scope.`,
		Args: cobra.ArbitraryArgs,
		RunE: runUpgrade,
	}

	cmd.Flags().Bool("dry-run", false, "Preview changes without writing files")

	common.AddTargetFlags(cmd)

	return cmd
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	// Same setup as plan/apply: SSH transport, target flags, reachability.
	clictx, _, err := common.ConnectContexts(cmd)
	if err != nil {
		return err
	}
	pr := clictx.Printer.(ui.StdPrinter)
	cfg, factory := clictx.Config, clictx.Factory

	// Create registry client.
	reg := registry.NewOCIClient(nil)

	// Build check inputs from all stacks.
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

	// Run the check inside the spinner so the user sees feedback immediately.
	var results []images.ImageStatus
	err = common.SpinnerOperation(pr, "Checking images...", func() error {
		localDigests := prefetchLocalDigests(cmd.Context(), inputs, makeLocalDigestFunc(cfg, factory, projectsByStack(inputs)))
		results, err = images.Check(cmd.Context(), inputs, reg, localDigests.lookup)
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

// renderUpgradeTerminal reports an upgrade as tables, like images check: one
// for the tags that were (or, in a dry run, would be) rewritten, one for the
// images that could not be upgraded and why, and a count of the rest.
func renderUpgradeTerminal(pr ui.Printer, results []images.ImageStatus, changes []images.FileChange, stackFiles map[string][]string, dryRun bool) {
	if len(results) == 0 {
		pr.Plain("No images found.")
		return
	}

	type changeKey struct{ stack, service string }
	changeMap := make(map[changeKey]images.FileChange, len(changes))
	for _, c := range changes {
		changeMap[changeKey{c.StackKey, c.Service}] = c
	}

	sorted := append([]images.ImageStatus(nil), results...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Stack != sorted[j].Stack {
			return sorted[i].Stack < sorted[j].Stack
		}
		return sorted[i].Service < sorted[j].Service
	})

	var upgraded, notUpgraded [][]tableCell
	var notes legend
	reason := func(label string) tableCell {
		notes.use(label)
		return tableCell{text: label, style: ui.YellowText}
	}
	latest := 0
	for _, r := range sorted {
		stack, image := plainCell(r.Stack), plainCell(imageNameWithoutTag(r.Image))
		tag := plainCell(r.CurrentTag)
		switch {
		case r.Error != "":
			notes.addError(r.Stack, imageNameWithoutTag(r.Image), r.Error)
			notUpgraded = append(notUpgraded, []tableCell{stack, image, tag, reason(labelError)})
		case len(r.NewerTags) > 0:
			to := tableCell{text: r.NewerTags[0], style: ui.YellowText}
			fc, changed := changeMap[changeKey{r.Stack, r.Service}]
			switch {
			case changed:
				upgraded = append(upgraded, []tableCell{stack, image, tag, to, plainCell(filepath.Base(fc.File))})
			case dryRun && len(stackFiles[r.Stack]) > 0:
				upgraded = append(upgraded, []tableCell{stack, image, tag, to})
			default:
				notUpgraded = append(notUpgraded, []tableCell{stack, image, tag, reason(labelTagNotFound)})
			}
		case r.NotApplied:
			notUpgraded = append(notUpgraded, []tableCell{stack, image, tag, reason(labelNotApplied)})
		case r.DigestStale:
			notUpgraded = append(notUpgraded, []tableCell{stack, image, tag, reason(labelDigest)})
		default:
			latest++
		}
	}

	printed := false
	section := func(line string) {
		if printed {
			pr.Plain("")
		}
		printed = true
		pr.Plain("%s", line)
	}

	if len(upgraded) > 0 {
		if dryRun {
			section(fmt.Sprintf("%s  %d image(s) would be upgraded (dry run)\n", ui.YellowText("↑"), len(upgraded)))
			printTable(pr, []string{"STACK", "IMAGE", "FROM", "TO"}, upgraded)
		} else {
			section(fmt.Sprintf("%s  %d image(s) upgraded\n", ui.GreenText("↑"), len(upgraded)))
			printTable(pr, []string{"STACK", "IMAGE", "FROM", "TO", "FILE"}, upgraded)
		}
	}
	if len(notUpgraded) > 0 {
		section(fmt.Sprintf("%s  %d image(s) not upgraded\n", ui.YellowText("⚠"), len(notUpgraded)))
		printTable(pr, []string{"STACK", "IMAGE", "TAG", "REASON"}, notUpgraded)
	}
	if latest > 0 {
		section(fmt.Sprintf("%s  %d image(s) already latest", ui.GreenText("✓"), latest))
	}
	notes.render(pr)

	// Footer: remind the user to apply the tag changes.
	if len(changes) > 0 && !dryRun {
		dim := lipgloss.NewStyle().Faint(true)
		cmdStyle := lipgloss.NewStyle().Italic(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#3478F6", Dark: "#4A9EFF"})
		pr.Plain("\n%s%s%s",
			dim.Render("Run "),
			cmdStyle.Render("dockform apply"),
			dim.Render(" to publish the changes."))
	}
}

// tableCell is one table cell: its text, and an optional style applied after
// padding so column widths come from the plain text.
type tableCell struct {
	text  string
	style func(string) string
}

func plainCell(s string) tableCell { return tableCell{text: s} }

// printTable prints rows under a faint bold header in the images check style,
// padding every column but the last to its widest cell.
func printTable(pr ui.Printer, header []string, rows [][]tableCell) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, c := range row {
			if len(c.text) > widths[i] {
				widths[i] = len(c.text)
			}
		}
	}
	pad := func(i int, s string) string {
		if i == len(header)-1 {
			return s
		}
		return fmt.Sprintf("%-*s", widths[i], s)
	}

	headerStyle := lipgloss.NewStyle().Faint(true).Bold(true)
	cols := make([]string, len(header))
	for i, h := range header {
		cols[i] = headerStyle.Render(pad(i, h))
	}
	pr.Plain("  %s", strings.Join(cols, "  "))
	for _, row := range rows {
		cols = cols[:0]
		for i, c := range row {
			cell := pad(i, c.text)
			if c.style != nil {
				cell = c.style(cell)
			}
			cols = append(cols, cell)
		}
		pr.Plain("  %s", strings.TrimRight(strings.Join(cols, "  "), " "))
	}
}
