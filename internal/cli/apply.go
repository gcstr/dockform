package cli

import (
	"context"
	"fmt"

	"github.com/gcstr/dockform/internal/config"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/planner"
	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the desired state",
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")
			cfg, err := config.Load(file)
			if err != nil {
				return err
			}
			prune, _ := cmd.Flags().GetBool("prune")

			d := dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
			pln, err := planner.NewWithDocker(d).BuildPlan(context.Background(), cfg)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), pln.String())

			if err := planner.NewWithDocker(d).Apply(context.Background(), cfg); err != nil {
				return err
			}

			if prune {
				// TODO: implement pruning of unmanaged resources with confirmation
				fmt.Fprintln(cmd.OutOrStdout(), "Prune requested: unmanaged resources will be removed (not yet implemented)")
			}
			return nil
		},
	}
	cmd.Flags().Bool("prune", false, "Delete unmanaged resources after confirmation")
	return cmd
}
