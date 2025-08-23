package validator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/config"
	"github.com/gcstr/dockform/internal/dockercli"
)

// Validate performs comprehensive validation of the user config and environment.
// - Verifies docker daemon connectivity for the configured context
// - Ensures application roots and referenced files exist (compose files, env files, sops secrets)
// - Verifies SOPS key file exists when SOPS is configured
func Validate(ctx context.Context, cfg config.Config, d *dockercli.Client) error {
	// 1) Docker daemon liveness
	if err := d.CheckDaemon(ctx); err != nil {
		return err
	}

	// 2) Root-level environment files
	if cfg.Environment != nil {
		for _, f := range cfg.Environment.Files {
			if f == "" {
				continue
			}
			p := f
			if !filepath.IsAbs(p) {
				p = filepath.Join(cfg.BaseDir, p)
			}
			if _, err := os.Stat(p); err != nil {
				return fmt.Errorf("env file %s: %w", f, err)
			}
		}
	}

	// 3) Root-level SOPS secrets
	if cfg.Secrets != nil && len(cfg.Secrets.Sops) > 0 {
		for _, s := range cfg.Secrets.Sops {
			if s.Path == "" {
				continue
			}
			p := s.Path
			if !filepath.IsAbs(p) {
				p = filepath.Join(cfg.BaseDir, p)
			}
			if _, err := os.Stat(p); err != nil {
				return fmt.Errorf("secrets sops file %s: %w", s.Path, err)
			}
		}
	}

	// 4) SOPS key file (if configured)
	if cfg.Sops != nil && cfg.Sops.Age != nil && cfg.Sops.Age.KeyFile != "" {
		key := cfg.Sops.Age.KeyFile
		if strings.HasPrefix(key, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				key = filepath.Join(home, key[2:])
			}
		}
		if _, err := os.Stat(key); err != nil {
			return fmt.Errorf("sops age key file: %w", err)
		}
	}

	// 5) Applications: roots and referenced files
	for appName, app := range cfg.Applications {
		// Root must exist
		if st, err := os.Stat(app.Root); err != nil || !st.IsDir() {
			if err != nil {
				return fmt.Errorf("application %s root: %w", appName, err)
			}
			return fmt.Errorf("application %s root is not a directory: %s", appName, app.Root)
		}
		// Compose files
		for _, f := range app.Files {
			p := f
			if !filepath.IsAbs(p) {
				p = filepath.Join(app.Root, p)
			}
			if _, err := os.Stat(p); err != nil {
				return fmt.Errorf("application %s compose file %s: %w", appName, f, err)
			}
		}
		// Env files (already rebased to app root semantics in config normalization)
		for _, e := range app.EnvFile {
			p := e
			if !filepath.IsAbs(p) {
				p = filepath.Join(app.Root, p)
			}
			if _, err := os.Stat(p); err != nil {
				return fmt.Errorf("application %s env file %s: %w", appName, e, err)
			}
		}
		// SOPS secrets (merged and rebased in config normalization)
		for _, s := range app.SopsSecrets {
			p := s.Path
			if p == "" {
				continue
			}
			if !filepath.IsAbs(p) {
				p = filepath.Join(app.Root, p)
			}
			if _, err := os.Stat(p); err != nil {
				return fmt.Errorf("application %s sops secret %s: %w", appName, s.Path, err)
			}
		}
	}

	return nil
}
