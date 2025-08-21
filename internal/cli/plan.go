package cli

import (
	"context"
	"fmt"

	"github.com/gcstr/dockform/internal/config"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/spf13/cobra"
)

func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show the plan to reach the desired state",
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")
			cfg, err := config.Load(file)
			if err != nil {
				return err
			}
			// Use Docker context from config and scope by identifier if present
			d := dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
			pln, err := planner.NewWithDocker(d).BuildPlan(context.Background(), cfg)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), pln.String())
			return nil
		},
	}
	return cmd
}
