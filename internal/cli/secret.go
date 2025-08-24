package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/config"
	"github.com/gcstr/dockform/internal/secrets"
	"github.com/spf13/cobra"
)

func newSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage SOPS secrets",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newSecretCreateCmd())
	return cmd
}

func newSecretCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <path>",
		Short: "Create a new SOPS-encrypted dotenv file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if cfg.Sops == nil || cfg.Sops.Age == nil || cfg.Sops.Age.KeyFile == "" {
				return errors.New("sops age.key_file is not configured in dockform config")
			}

			target := args[0]
			if !filepath.IsAbs(target) {
				cwd, _ := os.Getwd()
				target = filepath.Clean(filepath.Join(cwd, target))
			}
			if _, err := os.Stat(target); err == nil {
				return fmt.Errorf("file already exists: %s", target)
			}

			// Resolve recipients: start with configured list (if any),
			// then always include recipient(s) from keyfile, deduplicated in order.
			var recipients []string
			if cfg.Sops != nil && len(cfg.Sops.Recipients) > 0 {
				recipients = append(recipients, cfg.Sops.Recipients...)
			}
			r, err := secrets.AgeRecipientsFromKeyFile(cfg.Sops.Age.KeyFile)
			if err != nil {
				return err
			}
			recipients = append(recipients, r...)
			if len(recipients) == 0 {
				return errors.New("no age recipients configured or found in key file")
			}
			// Deduplicate while preserving order
			seen := make(map[string]struct{}, len(recipients))
			uniq := make([]string, 0, len(recipients))
			for _, rec := range recipients {
				rec = strings.TrimSpace(rec)
				if rec == "" {
					continue
				}
				if _, ok := seen[rec]; ok {
					continue
				}
				seen[rec] = struct{}{}
				uniq = append(uniq, rec)
			}
			recipients = uniq

			// Write plaintext template
			const template = "SECRET_KEY=secret\n"
			if err := os.WriteFile(target, []byte(template), 0o600); err != nil {
				return fmt.Errorf("write template: %w", err)
			}

			if err := secrets.EncryptDotenvFileWithSops(context.Background(), target, recipients, cfg.Sops.Age.KeyFile); err != nil {
				_ = os.Remove(target)
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created encrypted secret: %s\n", target)
			return nil
		},
	}
	return cmd
}
