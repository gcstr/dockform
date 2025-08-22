package cli

import (
	"context"
	"fmt"

	"strings"

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
			out := pln.String()
			fmt.Fprintln(cmd.OutOrStdout(), out)
			if !prune && strings.Contains(out, "[remove]") {
				fmt.Fprintln(cmd.OutOrStdout(), "No resources will be removed. Include --prune to delete them")
			}

			if err := planner.NewWithDocker(d).Apply(context.Background(), cfg); err != nil {
				return err
			}

			if prune {
				if err := planner.NewWithDocker(d).Prune(context.Background(), cfg); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("prune", false, "Delete unmanaged resources after confirmation")
	return cmd
}
