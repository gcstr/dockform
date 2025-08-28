package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/config"
	"github.com/gcstr/dockform/internal/secrets"
	decrypt "github.com/getsops/sops/v3/decrypt"
	"github.com/spf13/cobra"
)

func newSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage SOPS secrets",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newSecretCreateCmd())
	cmd.AddCommand(newSecretRekeyCmd())
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
				return apperr.New("cli.secret.create", apperr.InvalidInput, "sops age.key_file is not configured in dockform config")
			}

			target := args[0]
			if !filepath.IsAbs(target) {
				cwd, _ := os.Getwd()
				target = filepath.Clean(filepath.Join(cwd, target))
			}
			if _, err := os.Stat(target); err == nil {
				return apperr.New("cli.secret.create", apperr.Conflict, "file already exists: %s", target)
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
				return apperr.New("cli.secret.create", apperr.InvalidInput, "no age recipients configured or found in key file")
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
				return apperr.Wrap("cli.secret.create", apperr.Internal, err, "write template")
			}

			if err := secrets.EncryptDotenvFileWithSops(context.Background(), target, recipients, cfg.Sops.Age.KeyFile); err != nil {
				_ = os.Remove(target)
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Created encrypted secret: %s\n", target); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}

func newSecretRekeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rekey",
		Short: "Re-encrypt all declared SOPS secret files with configured recipients",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if cfg.Sops == nil || cfg.Sops.Age == nil || cfg.Sops.Age.KeyFile == "" {
				return apperr.New("cli.secret.rekey", apperr.InvalidInput, "sops age.key_file is not configured in dockform config")
			}

			// Resolve recipients: configured list + keyfile-derived, deduped.
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
				return apperr.New("cli.secret.rekey", apperr.InvalidInput, "no age recipients configured or found in key file")
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

			// Collect unique absolute secret file paths and formats from root and applications
			type secretItem struct {
				path   string
				format string
			}
			items := make([]secretItem, 0)
			dedup := map[string]struct{}{}

			if cfg.Secrets != nil {
				for _, s := range cfg.Secrets.Sops {
					if s.Path == "" {
						continue
					}
					abs := filepath.Clean(filepath.Join(cfg.BaseDir, s.Path))
					if _, ok := dedup[abs]; ok {
						continue
					}
					dedup[abs] = struct{}{}
					f := strings.ToLower(strings.TrimSpace(s.Format))
					if f == "" {
						f = "dotenv"
					}
					items = append(items, secretItem{path: abs, format: f})
				}
			}
			for _, app := range cfg.Applications {
				for _, s := range app.SopsSecrets {
					if s.Path == "" {
						continue
					}
					abs := filepath.Clean(filepath.Join(app.Root, s.Path))
					if _, ok := dedup[abs]; ok {
						continue
					}
					dedup[abs] = struct{}{}
					f := strings.ToLower(strings.TrimSpace(s.Format))
					if f == "" {
						f = "dotenv"
					}
					items = append(items, secretItem{path: abs, format: f})
				}
			}

			if len(items) == 0 {
				return nil
			}

			// Ensure SOPS_AGE_KEY_FILE set for decrypt compatibility
			key := cfg.Sops.Age.KeyFile
			if strings.HasPrefix(key, "~/") {
				if home, err := os.UserHomeDir(); err == nil {
					key = filepath.Join(home, key[2:])
				}
			}
			prev := os.Getenv("SOPS_AGE_KEY_FILE")
			unset := prev == ""
			_ = os.Setenv("SOPS_AGE_KEY_FILE", key)
			if unset {
				defer os.Unsetenv("SOPS_AGE_KEY_FILE")
			} else {
				defer os.Setenv("SOPS_AGE_KEY_FILE", prev)
			}

			cwd, _ := os.Getwd()

			for _, it := range items {
				if it.format != "dotenv" {
					return apperr.New("cli.secret.rekey", apperr.InvalidInput, "unsupported secrets format %q for %s: only \"dotenv\" is supported", it.format, it.path)
				}
				// Decrypt existing file
				plaintext, err := decrypt.File(it.path, it.format)
				if err != nil {
					return apperr.Wrap("cli.secret.rekey", apperr.External, err, "sops decrypt %s", it.path)
				}

				// Write plaintext to a temp file in the same directory
				dir := filepath.Dir(it.path)
				tmpf, err := os.CreateTemp(dir, ".rekey-*.env")
				if err != nil {
					return apperr.Wrap("cli.secret.rekey", apperr.Internal, err, "create temp file")
				}
				tmp := tmpf.Name()
				_ = tmpf.Chmod(0o600)
				if _, err := tmpf.Write(plaintext); err != nil {
					tmpf.Close()
					_ = os.Remove(tmp)
					return apperr.Wrap("cli.secret.rekey", apperr.Internal, err, "write temp plaintext")
				}
				if err := tmpf.Close(); err != nil {
					_ = os.Remove(tmp)
					return apperr.Wrap("cli.secret.rekey", apperr.Internal, err, "close temp plaintext")
				}

				// Encrypt plaintext temp file with new recipients
				if err := secrets.EncryptDotenvFileWithSops(context.Background(), tmp, recipients, cfg.Sops.Age.KeyFile); err != nil {
					_ = os.Remove(tmp)
					return err
				}

				// Replace original file atomically
				if err := os.Rename(tmp, it.path); err != nil {
					_ = os.Remove(tmp)
					return apperr.Wrap("cli.secret.rekey", apperr.Internal, err, "replace original %s", it.path)
				}

				// Print relative path info
				rel := it.path
				if r, err := filepath.Rel(cwd, it.path); err == nil {
					rel = r
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s reencrypted\n", rel); err != nil {
					return err
				}
			}

			return nil
		},
	}
	return cmd
}
