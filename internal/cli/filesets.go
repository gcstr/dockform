package cli

import (
	"strings"

	"github.com/gcstr/dockform/internal/manifest"
	"github.com/spf13/cobra"
)

func newFilesetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "filesets",
		Short: "Fileset-only operations",
	}
	cmd.AddCommand(newFilesetPlanCmd())
	cmd.AddCommand(newFilesetApplyCmd())
	return cmd
}

func newFilesetPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show fileset diffs only",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Setup CLI context with all standard initialization
			ctx, err := SetupCLIContext(cmd)
			if err != nil {
				return err
			}

			// Configure sequential processing if requested (default is parallel)
			sequential, _ := cmd.Flags().GetBool("sequential")
			if sequential {
				ctx.Planner = ctx.Planner.WithParallel(false)
			}

			// Build the plan
			plan, err := ctx.BuildPlan()
			if err != nil {
				return err
			}

			// Filter plan output to only fileset lines
			out := plan.String()
			filtered := filterFilesetLines(out)
			ctx.Printer.Plain("%s", filtered)
			return nil
		},
	}
	cmd.Flags().Bool("sequential", false, "Use sequential processing instead of the default parallel processing (slower but uses less CPU and Docker daemon resources)")
	return cmd
}

func newFilesetApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply fileset diffs only",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Setup CLI context with all standard initialization
			ctx, err := SetupCLIContext(cmd)
			if err != nil {
				return err
			}

			// Configure sequential processing if requested (default is parallel)
			sequential, _ := cmd.Flags().GetBool("sequential")
			if sequential {
				ctx.Planner = ctx.Planner.WithParallel(false)
			}

			// Build the plan and show filtered output
			plan, err := ctx.BuildPlan()
			if err != nil {
				return err
			}

			// Print only fileset lines of the plan
			out := plan.String()
			ctx.Printer.Plain("%s", filterFilesetLines(out))

			// Apply only the fileset part. We constrain to filesets by
			// temporarily clearing applications from config.
			cfgApps := ctx.Config.Applications
			ctx.Config.Applications = map[string]manifest.Application{}
			defer func() { ctx.Config.Applications = cfgApps }()

			if err := ctx.ApplyPlan(); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().Bool("sequential", false, "Use sequential processing instead of the default parallel processing (slower but uses less CPU and Docker daemon resources)")
	return cmd
}

func filterFilesetLines(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\r\n"), "\n")
	out := make([]string, 0, len(lines))
	inFilesetSection := false
	foundFilesetContent := false

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)

		// Check if we're entering the Filesets section
		if trimmed == "Filesets" {
			inFilesetSection = true
			out = append(out, l)
			continue
		}

		// Check if we're entering a new section (starts with capital letter and no indentation)
		if len(l) > 0 && l[0] != ' ' && l[0] != '\t' && trimmed != "" && trimmed != "Filesets" {
			inFilesetSection = false
			continue
		}

		// If we're in the filesets section, include the line
		if inFilesetSection && trimmed != "" {
			out = append(out, l)
			// Check if this line has an actual fileset action (contains icons)
			if strings.Contains(l, "↑") || strings.Contains(l, "×") || strings.Contains(l, "→") || strings.Contains(l, "✓") {
				foundFilesetContent = true
			}
		}
	}

	// If we only have the header or no real fileset content, return no-op message
	if len(out) <= 1 || !foundFilesetContent {
		return "[no-op] no filesets defined or no fileset changes"
	}

	return strings.Join(out, "\n")
}
