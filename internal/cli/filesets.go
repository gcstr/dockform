package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/validator"
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
			file, _ := cmd.Flags().GetString("config")
			cfg, err := manifest.Load(file)
			if err != nil {
				return err
			}
			// Use Docker context from config and scope by identifier if present
			d := dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
			if err := validator.Validate(context.Background(), cfg, d); err != nil {
				return err
			}
			pln, err := planner.NewWithDocker(d).BuildPlan(context.Background(), cfg)
			if err != nil {
				return err
			}
			// Filter plan output to only fileset lines
			out := pln.String()
			filtered := filterFilesetLines(out)
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), filtered); err != nil {
				return err
			}
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
			file, _ := cmd.Flags().GetString("config")
			cfg, err := manifest.Load(file)
			if err != nil {
				return err
			}
			d := dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
			if err := validator.Validate(context.Background(), cfg, d); err != nil {
				return err
			}
			pln, err := planner.NewWithDocker(d).BuildPlan(context.Background(), cfg)
			if err != nil {
				return err
			}
			// Print only fileset lines of the plan
			out := pln.String()
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), filterFilesetLines(out)); err != nil {
				return err
			}
			// Apply only the fileset part. We reuse Planner.Apply but constrain to filesets
			// by creating a copy of config with applications cleared so only filesets + top-level are touched.
			cfgApps := cfg.Applications
			cfg.Applications = map[string]manifest.Application{}
			defer func() { cfg.Applications = cfgApps }()
			if err := planner.NewWithDocker(d).Apply(context.Background(), cfg); err != nil {
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
