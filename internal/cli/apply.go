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

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the desired state",
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("config")
			cfg, err := config.Load(file)
			if err != nil {
				return err
			}
			prune, _ := cmd.Flags().GetBool("prune")

			d := dockercli.New(cfg.Docker.Context).WithIdentifier(cfg.Docker.Identifier)
			if err := validator.Validate(context.Background(), cfg, d); err != nil {
				return err
			}
			pln, err := planner.NewWithDocker(d).BuildPlan(context.Background(), cfg)
			if err != nil {
				return err
			}
			out := pln.String()
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), out); err != nil {
				return err
			}
			if !prune && strings.Contains(out, "[remove]") {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "No resources will be removed. Include --prune to delete them"); err != nil {
					return err
				}
			}

			// Skip Apply when there are no add/change operations and no assets configured
			if !strings.Contains(out, "[add]") && !strings.Contains(out, "[change]") && len(cfg.Assets) == 0 {
				return nil
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
