package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
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
	cmd.AddCommand(newSecretRekeyCmd())
	cmd.AddCommand(newSecretEditCmd())
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

			// Debug: show sops command about to run
			if verbose {
				key := cfg.Sops.Age.KeyFile
				if strings.HasPrefix(key, "~/") {
					if home, err := os.UserHomeDir(); err == nil {
						key = filepath.Join(home, key[2:])
					}
				}
				args := []string{"--encrypt", "--input-type", "dotenv", "--output-type", "dotenv", "--in-place"}
				for _, rec := range recipients {
					args = append(args, "--age", rec)
				}
				args = append(args, target)
				fmt.Fprintf(cmd.ErrOrStderr(), "DEBUG: SOPS_AGE_KEY_FILE=%s sops %s\n", key, strings.Join(args, " "))
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

			// Collect unique absolute secret file paths from root and applications
			type secretItem struct {
				path string
			}
			items := make([]secretItem, 0)
			dedup := map[string]struct{}{}

			if cfg.Secrets != nil {
				for _, sp := range cfg.Secrets.Sops {
					if sp == "" {
						continue
					}
					abs := filepath.Clean(filepath.Join(cfg.BaseDir, sp))
					if _, ok := dedup[abs]; ok {
						continue
					}
					dedup[abs] = struct{}{}
					items = append(items, secretItem{path: abs})
				}
			}
			for _, app := range cfg.Applications {
				for _, sp := range app.SopsSecrets {
					if sp == "" {
						continue
					}
					abs := filepath.Clean(filepath.Join(app.Root, sp))
					if _, ok := dedup[abs]; ok {
						continue
					}
					dedup[abs] = struct{}{}
					items = append(items, secretItem{path: abs})
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
				defer func() { _ = os.Unsetenv("SOPS_AGE_KEY_FILE") }()
			} else {
				defer func() { _ = os.Setenv("SOPS_AGE_KEY_FILE", prev) }()
			}

			cwd, _ := os.Getwd()

			// Verify sops binary exists once
			if _, err := exec.LookPath("sops"); err != nil {
				return apperr.New("cli.secret.rekey", apperr.NotFound, "sops binary not found on PATH; please install sops")
			}

			for _, it := range items {
				// Debug: show decrypt command
				if verbose {
					fmt.Fprintf(cmd.ErrOrStderr(), "DEBUG: SOPS_AGE_KEY_FILE=%s sops --decrypt --input-type dotenv %s\n", key, it.path)
				}
				// Decrypt existing file (dotenv) using system sops
				c := exec.CommandContext(cmd.Context(), "sops", "--decrypt", "--input-type", "dotenv", it.path)
				c.Env = os.Environ()
				plaintext, err := c.Output()
				if err != nil {
					if ee, ok := err.(*exec.ExitError); ok {
						return apperr.Wrap("cli.secret.rekey", apperr.External, fmt.Errorf("%s", string(ee.Stderr)), "sops decrypt %s", it.path)
					}
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
					_ = tmpf.Close()
					_ = os.Remove(tmp)
					return apperr.Wrap("cli.secret.rekey", apperr.Internal, err, "write temp plaintext")
				}
				if err := tmpf.Close(); err != nil {
					_ = os.Remove(tmp)
					return apperr.Wrap("cli.secret.rekey", apperr.Internal, err, "close temp plaintext")
				}

				// Debug: show encrypt command about to run
				if verbose {
					args := []string{"--encrypt", "--input-type", "dotenv", "--output-type", "dotenv", "--in-place"}
					for _, rec := range recipients {
						args = append(args, "--age", rec)
					}
					args = append(args, tmp)
					fmt.Fprintf(cmd.ErrOrStderr(), "DEBUG: SOPS_AGE_KEY_FILE=%s sops %s\n", key, strings.Join(args, " "))
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

func newSecretEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <path>",
		Short: "Edit a SOPS-encrypted dotenv file interactively",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if cfg.Sops == nil || cfg.Sops.Age == nil || cfg.Sops.Age.KeyFile == "" {
				return apperr.New("cli.secret.edit", apperr.InvalidInput, "sops age.key_file is not configured in dockform config")
			}

			target := args[0]
			if !filepath.IsAbs(target) {
				cwd, _ := os.Getwd()
				target = filepath.Clean(filepath.Join(cwd, target))
			}
			if _, err := os.Stat(target); err != nil {
				return apperr.Wrap("cli.secret.edit", apperr.NotFound, err, "secret not found: %s", target)
			}

			// Ensure sops binary exists
			if _, err := exec.LookPath("sops"); err != nil {
				return apperr.New("cli.secret.edit", apperr.NotFound, "sops binary not found on PATH; please install sops")
			}

			// Ensure SOPS_AGE_KEY_FILE is set for editing
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
				defer func() { _ = os.Unsetenv("SOPS_AGE_KEY_FILE") }()
			} else {
				defer func() { _ = os.Setenv("SOPS_AGE_KEY_FILE", prev) }()
			}

			if verbose {
				fmt.Fprintf(cmd.ErrOrStderr(), "DEBUG: SOPS_AGE_KEY_FILE=%s sops --input-type dotenv --output-type dotenv %s\n", key, target)
			}

			c := exec.CommandContext(cmd.Context(), "sops", "--input-type", "dotenv", "--output-type", "dotenv", target)
			c.Env = os.Environ()
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					return apperr.Wrap("cli.secret.edit", apperr.External, fmt.Errorf("%s", string(ee.Stderr)), "sops edit %s", target)
				}
				return apperr.Wrap("cli.secret.edit", apperr.External, err, "sops edit %s", target)
			}
			return nil
		},
	}
	return cmd
}
