package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/gcstr/dockform/internal/config"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/validator"
	"github.com/spf13/cobra"
)

func newAssetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "asset",
		Short: "Asset-only operations",
	}
	cmd.AddCommand(newAssetPlanCmd())
	cmd.AddCommand(newAssetApplyCmd())
	return cmd
}

func newAssetPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show asset diffs only",
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("config")
			cfg, err := config.Load(file)
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
			// Filter plan output to only asset lines
			out := pln.String()
			filtered := filterAssetLines(out)
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), filtered); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}

func newAssetApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply asset diffs only",
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("config")
			cfg, err := config.Load(file)
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
			// Print only asset lines of the plan
			out := pln.String()
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), filterAssetLines(out)); err != nil {
				return err
			}
			// Apply only the asset part. We reuse Planner.Apply but constrain to assets
			// by creating a copy of config with applications cleared so only assets + top-level are touched.
			cfgApps := cfg.Applications
			cfg.Applications = map[string]config.Application{}
			defer func() { cfg.Applications = cfgApps }()
			if err := planner.NewWithDocker(d).Apply(context.Background(), cfg); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}

func filterAssetLines(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\r\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		// keep lines starting with asset plan messages
		if strings.Contains(l, "asset ") {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return "[no-op] no assets defined or no asset changes"
	}
	return strings.Join(out, "\n")
}
