package validator

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/dockercli"
	"github.com/gcstr/dockform/internal/manifest"
)

// Validate performs comprehensive validation of the user config and environment.
// - Verifies docker daemon liveness for the configured context
// - Ensures stack roots and referenced files exist (compose files, env files, sops secrets)
// - Verifies SOPS key file exists when SOPS is configured
func Validate(ctx context.Context, cfg manifest.Config, d *dockercli.Client) error {
	// 1) Docker daemon liveness
	if err := d.CheckDaemon(ctx); err != nil {
		// Check if this is a context cancellation - if so, return it directly
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}

	// 1.1) docker.identifier validation: only letters, numbers, hyphen
	if cfg.Docker.Identifier != "" {
		validIdent := regexp.MustCompile(`^[A-Za-z0-9-]+$`)
		if !validIdent.MatchString(cfg.Docker.Identifier) {
			return apperr.New("validator.Validate", apperr.InvalidInput, "docker.identifier: must match [A-Za-z0-9-]+")
		}
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
				return apperr.Wrap("validator.Validate", apperr.NotFound, err, "environment file %s not found", f)
			}
		}
	}

	// 3) Root-level SOPS secrets
	if cfg.Secrets != nil && len(cfg.Secrets.Sops) > 0 {
		for _, sp := range cfg.Secrets.Sops {
			if sp == "" {
				continue
			}
			p := sp
			if !filepath.IsAbs(p) {
				p = filepath.Join(cfg.BaseDir, p)
			}
			if _, err := os.Stat(p); err != nil {
				return apperr.Wrap("validator.Validate", apperr.NotFound, err, "SOPS secret file %s not found", sp)
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
			return apperr.Wrap("validator.Validate", apperr.NotFound, err, "SOPS age key file %s not found", key)
		}
	}

	// 5) Stacks: roots and referenced files
	for stackName, stack := range cfg.Stacks {
		// Root must exist
		if st, err := os.Stat(stack.Root); err != nil || !st.IsDir() {
			if err != nil {
				return apperr.Wrap("validator.Validate", apperr.NotFound, err, "stack %s root", stackName)
			}
			return apperr.New("validator.Validate", apperr.InvalidInput, "stack %s root is not a directory: %s", stackName, stack.Root)
		}
		// Compose files
		for _, f := range stack.Files {
			p := f
			if !filepath.IsAbs(p) {
				p = filepath.Join(stack.Root, p)
			}
			if _, err := os.Stat(p); err != nil {
				return apperr.Wrap("validator.Validate", apperr.NotFound, err, "stack %s compose file %s", stackName, f)
			}
		}

		// Validate compose file syntax by attempting to parse it with Docker
		if _, err := d.ComposeConfigFull(ctx, stack.Root, stack.Files, stack.Profiles, []string{}, []string{}); err != nil {
			// Check if this is a context cancellation error - if so, return it directly
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if len(stack.Files) == 1 {
				return apperr.Wrap("validator.Validate", apperr.External, err, "invalid compose file %s for stack %s", stack.Files[0], stackName)
			} else if len(stack.Files) > 1 {
				return apperr.Wrap("validator.Validate", apperr.External, err, "invalid compose files %v for stack %s", stack.Files, stackName)
			}
			return apperr.Wrap("validator.Validate", apperr.External, err, "invalid compose file for stack %s", stackName)
		}
		// Env files (already rebased to stack root semantics in config normalization)
		for _, e := range stack.EnvFile {
			p := e
			if !filepath.IsAbs(p) {
				p = filepath.Join(stack.Root, p)
			}
			if _, err := os.Stat(p); err != nil {
				return apperr.Wrap("validator.Validate", apperr.NotFound, err, "stack %s env file %s", stackName, e)
			}
		}
		// SOPS secrets (merged and rebased in config normalization)
		for _, sp := range stack.SopsSecrets {
			p := sp
			if p == "" {
				continue
			}
			if !filepath.IsAbs(p) {
				p = filepath.Join(stack.Root, p)
			}
			if _, err := os.Stat(p); err != nil {
				return apperr.Wrap("validator.Validate", apperr.NotFound, err, "stack %s sops secret %s", stackName, sp)
			}
		}
	}

	// 6) Filesets: ensure sources exist and are directories
	for name, a := range cfg.Filesets {
		if a.SourceAbs == "" {
			return apperr.Wrap("validator.Validate", apperr.InvalidInput, manifest.ErrMissingRequired, "fileset %s: source path is required", name)
		}
		st, err := os.Stat(a.SourceAbs)
		if err != nil {
			return apperr.Wrap("validator.Validate", apperr.NotFound, err, "fileset %s source", name)
		}
		if !st.IsDir() {
			return apperr.New("validator.Validate", apperr.InvalidInput, "fileset %s source is not a directory: %s", name, a.SourceAbs)
		}
	}

	return nil
}
