package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/secrets"
	"github.com/gcstr/dockform/internal/ui"
	"github.com/spf13/cobra"
)

func newSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage SOPS secrets",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newSecretCreateCmd())
	cmd.AddCommand(newSecretRekeyCmd())
	cmd.AddCommand(newSecretDecryptCmd())
	cmd.AddCommand(newSecretEditCmd())
	return cmd
}

func resolveRecipientsAndKey(cfg manifest.Config) ([]string, string, error) {
	if cfg.Sops == nil || cfg.Sops.Age == nil || cfg.Sops.Age.KeyFile == "" {
		return nil, "", apperr.New("cli.resolveRecipientsAndKey", apperr.InvalidInput, "sops age key_file not configured")
	}
	recipients := cfg.Sops.Recipients
	if len(recipients) == 0 {
		r, err := secrets.AgeRecipientsFromKeyFile(cfg.Sops.Age.KeyFile)
		if err != nil {
			return nil, "", err
		}
		recipients = r
	}
	return recipients, cfg.Sops.Age.KeyFile, nil
}

func newSecretCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <path>",
		Short: "Create a new SOPS-encrypted dotenv file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
			cfg, err := manifest.Load(cfgPath)
			if err != nil && cfgPath == "" && apperr.IsKind(err, apperr.NotFound) {
				if selPath, ok, selErr := selectManifestPath(cmd, pr, ".", 3); selErr == nil && ok {
					_ = cmd.Flags().Set("config", selPath)
					cfg, err = manifest.Load(selPath)
				} else if selErr != nil {
					return selErr
				}
			}
			if err != nil {
				return err
			}
			recipients, keyFile, err := resolveRecipientsAndKey(cfg)
			if err != nil {
				return err
			}
			path := args[0]
			if _, err := os.Stat(path); err == nil {
				return apperr.New("cli.newSecretCreateCmd", apperr.InvalidInput, "target exists: %s", path)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			// Seed default plaintext content
			if err := os.WriteFile(path, []byte("SECRET_KEY=secret\n"), 0o600); err != nil {
				return err
			}
			if err := secrets.EncryptDotenvFileWithSops(context.Background(), path, recipients, keyFile); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "secret created:", path); err != nil {
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
			pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
			cfg, err := manifest.Load(cfgPath)
			if err != nil && cfgPath == "" && apperr.IsKind(err, apperr.NotFound) {
				if selPath, ok, selErr := selectManifestPath(cmd, pr, ".", 3); selErr == nil && ok {
					_ = cmd.Flags().Set("config", selPath)
					cfg, err = manifest.Load(selPath)
				} else if selErr != nil {
					return selErr
				}
			}
			if err != nil {
				return err
			}
			recipients, keyFile, err := resolveRecipientsAndKey(cfg)
			if err != nil {
				return err
			}
			if cfg.Secrets == nil || len(cfg.Secrets.Sops) == 0 {
				return nil
			}
			for _, p := range cfg.Secrets.Sops {
				path := p
				if !filepath.IsAbs(path) {
					path = filepath.Join(cfg.BaseDir, path)
				}
				pairs, err := secrets.DecryptAndParse(cmd.Context(), path, keyFile)
				if err != nil {
					return err
				}
				plain := strings.Join(pairs, "\n") + "\n"
				if err := os.WriteFile(path, []byte(plain), 0o600); err != nil {
					return err
				}
				if err := secrets.EncryptDotenvFileWithSops(cmd.Context(), path, recipients, keyFile); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s reencrypted\n", p); err != nil {
					return err
				}
			}
			return nil
		},
	}
	return cmd
}

func newSecretDecryptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decrypt <path>",
		Short: "Decrypt a SOPS-encrypted dotenv file and print to stdout",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
			cfg, err := manifest.Load(cfgPath)
			if err != nil && cfgPath == "" && apperr.IsKind(err, apperr.NotFound) {
				if selPath, ok, selErr := selectManifestPath(cmd, pr, ".", 3); selErr == nil && ok {
					_ = cmd.Flags().Set("config", selPath)
					cfg, err = manifest.Load(selPath)
				} else if selErr != nil {
					return selErr
				}
			}
			if err != nil {
				return err
			}
			pairs, err := secrets.DecryptAndParse(cmd.Context(), args[0], cfg.Sops.Age.KeyFile)
			if err != nil {
				return err
			}
			content := strings.Join(pairs, "\n") + "\n"
			if _, err := fmt.Fprint(cmd.OutOrStdout(), content); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}

func newSecretEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <path>",
		Short: "Edit a SOPS-encrypted dotenv file interactively",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			pr := ui.StdPrinter{Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
			cfg, err := manifest.Load(cfgPath)
			if err != nil && cfgPath == "" && apperr.IsKind(err, apperr.NotFound) {
				if selPath, ok, selErr := selectManifestPath(cmd, pr, ".", 3); selErr == nil && ok {
					_ = cmd.Flags().Set("config", selPath)
					cfg, err = manifest.Load(selPath)
				} else if selErr != nil {
					return selErr
				}
			}
			if err != nil {
				return err
			}
			recipients, keyFile, err := resolveRecipientsAndKey(cfg)
			if err != nil {
				return err
			}
			path := args[0]
			tmp, err := os.CreateTemp("", "dockform-secret-*.env")
			if err != nil {
				return err
			}
			defer func() { _ = os.Remove(tmp.Name()) }()
			pairs, err := secrets.DecryptAndParse(cmd.Context(), path, keyFile)
			if err != nil {
				return err
			}
			plain := strings.Join(pairs, "\n") + "\n"
			if err := os.WriteFile(tmp.Name(), []byte(plain), 0o600); err != nil {
				return err
			}
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			c := exec.Command(editor, tmp.Name())
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return err
			}
			// Overwrite original with edited plaintext, then encrypt in-place
			b, err := os.ReadFile(tmp.Name())
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, b, 0o600); err != nil {
				return err
			}
			if err := secrets.EncryptDotenvFileWithSops(cmd.Context(), path, recipients, keyFile); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "secret updated:", path); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}
