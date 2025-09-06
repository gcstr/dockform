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
	return cmd
}

func filterFilesetLines(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\r\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		// keep lines starting with fileset plan messages
		if strings.Contains(l, "fileset ") {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return "[no-op] no filesets defined or no fileset changes"
	}
	return strings.Join(out, "\n")
}
