package cli

import (
	"context"
	"fmt"

	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/validator"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration and environment",
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
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "validation successful"); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}
