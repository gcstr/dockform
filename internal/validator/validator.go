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
// For multi-context configs, it validates each context's context and all stacks.
func Validate(ctx context.Context, cfg manifest.Config, factory *dockercli.DefaultClientFactory) error {
	// 1) Validate each context's Docker context is reachable
	for contextName := range cfg.Contexts {
		client := factory.GetClientForContext(contextName, &cfg)
		if err := client.CheckDaemon(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return apperr.Wrap("validator.Validate", apperr.Unavailable, err, "context %s", contextName)
		}
	}

	// Validate identifier format (project-wide)
	if cfg.Identifier != "" {
		validIdent := regexp.MustCompile(`^[A-Za-z0-9-]+$`)
		if !validIdent.MatchString(cfg.Identifier) {
			return apperr.New("validator.Validate", apperr.InvalidInput, "identifier: must match [A-Za-z0-9-]+")
		}
	}

	// 2) Check if any SOPS secrets are configured (in any stack)
	hasSopsSecrets := false
	allStacks := cfg.GetAllStacks()
	for _, stack := range allStacks {
		if len(stack.SopsSecrets) > 0 {
			for _, s := range stack.SopsSecrets {
				if s != "" {
					hasSopsSecrets = true
					break
				}
			}
		}
		if hasSopsSecrets {
			break
		}
	}

	// 3) SOPS key file validation
	if hasSopsSecrets && cfg.Sops != nil && cfg.Sops.Age != nil {
		// Check if key_file is empty - this indicates a missing environment variable
		if cfg.Sops.Age.KeyFile == "" {
			return apperr.New("validator.Validate", apperr.InvalidInput,
				"SOPS age key_file is empty but SOPS secrets are configured; "+
					"if using environment variable interpolation (e.g., ${AGE_KEY_FILE}), "+
					"ensure the variable is set in your environment")
		}

		// Validate that the key file exists
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

	// 4) Validate all stacks (discovered + explicit)
	for stackKey, stack := range allStacks {
		contextName, stackName, err := manifest.ParseStackKey(stackKey)
		if err != nil {
			return apperr.Wrap("validator.Validate", apperr.InvalidInput, err, "invalid stack key %s", stackKey)
		}

		// Get context config and client
		_, ok := cfg.Contexts[contextName]
		if !ok {
			return apperr.New("validator.Validate", apperr.InvalidInput, "stack %s references unknown context %s", stackKey, contextName)
		}
		client := factory.GetClientForContext(contextName, &cfg)

		// Root must exist
		if stack.Root != "" {
			if st, err := os.Stat(stack.Root); err != nil || !st.IsDir() {
				if err != nil {
					return apperr.Wrap("validator.Validate", apperr.NotFound, err, "stack %s root", stackKey)
				}
				return apperr.New("validator.Validate", apperr.InvalidInput, "stack %s root is not a directory: %s", stackKey, stack.Root)
			}
		}

		// Compose files
		for _, f := range stack.Files {
			p := f
			if !filepath.IsAbs(p) && stack.Root != "" {
				p = filepath.Join(stack.Root, p)
			}
			if _, err := os.Stat(p); err != nil {
				return apperr.Wrap("validator.Validate", apperr.NotFound, err, "stack %s compose file %s", stackKey, f)
			}
		}

		// Validate compose file syntax by attempting to parse it with Docker
		// Note: We pass env files but skip inline env (which includes SOPS secrets) to avoid
		// slow decryption and key availability issues. This means stacks relying on SOPS
		// secrets for variable interpolation may fail validation but work at apply.
		// See TECHNICAL_DEBT.md for details.
		if len(stack.Files) > 0 && stack.Root != "" {
			if _, err := client.ComposeConfigFull(ctx, stack.Root, stack.Files, stack.Profiles, stack.EnvFile, []string{}); err != nil {
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
		}

		// Env files (already rebased to stack root semantics in config normalization)
		for _, e := range stack.EnvFile {
			p := e
			if !filepath.IsAbs(p) && stack.Root != "" {
				p = filepath.Join(stack.Root, p)
			}
			if _, err := os.Stat(p); err != nil {
				return apperr.Wrap("validator.Validate", apperr.NotFound, err, "stack %s env file %s", stackKey, e)
			}
		}

		// SOPS secrets (merged and rebased in config normalization)
		for _, sp := range stack.SopsSecrets {
			p := sp
			if p == "" {
				continue
			}
			if !filepath.IsAbs(p) && stack.Root != "" {
				p = filepath.Join(stack.Root, p)
			}
			if _, err := os.Stat(p); err != nil {
				return apperr.Wrap("validator.Validate", apperr.NotFound, err, "stack %s sops secret %s", stackKey, sp)
			}
		}
	}

	// 5) Validate discovered filesets
	for name, fs := range cfg.GetAllFilesets() {
		if fs.SourceAbs == "" {
			return apperr.New("validator.Validate", apperr.InvalidInput, "fileset %s: source path is required", name)
		}
		st, err := os.Stat(fs.SourceAbs)
		if err != nil {
			return apperr.Wrap("validator.Validate", apperr.NotFound, err, "fileset %s source", name)
		}
		if !st.IsDir() {
			return apperr.New("validator.Validate", apperr.InvalidInput, "fileset %s source is not a directory: %s", name, fs.SourceAbs)
		}
	}

	return nil
}

// ValidateContext validates a single context's configuration.
// This is useful for targeted validation when using --context flag.
func ValidateContext(ctx context.Context, cfg manifest.Config, contextName string, client *dockercli.Client) error {
	_, ok := cfg.Contexts[contextName]
	if !ok {
		return apperr.New("validator.ValidateContext", apperr.InvalidInput, "unknown context: %s", contextName)
	}

	// Check context is reachable
	if err := client.CheckDaemon(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return apperr.Wrap("validator.ValidateContext", apperr.Unavailable, err, "context %s", contextName)
	}

	// Identifier validation is done at project level, not per-context

	// Validate stacks for this context
	for stackName, stack := range cfg.GetStacksForContext(contextName) {
		stackKey := manifest.MakeStackKey(contextName, stackName)

		// Root must exist
		if stack.Root != "" {
			if st, err := os.Stat(stack.Root); err != nil || !st.IsDir() {
				if err != nil {
					return apperr.Wrap("validator.ValidateDaemon", apperr.NotFound, err, "stack %s root", stackKey)
				}
				return apperr.New("validator.ValidateDaemon", apperr.InvalidInput, "stack %s root is not a directory: %s", stackKey, stack.Root)
			}
		}

		// Compose files
		for _, f := range stack.Files {
			p := f
			if !filepath.IsAbs(p) && stack.Root != "" {
				p = filepath.Join(stack.Root, p)
			}
			if _, err := os.Stat(p); err != nil {
				return apperr.Wrap("validator.ValidateDaemon", apperr.NotFound, err, "stack %s compose file %s", stackKey, f)
			}
		}

		// Validate compose file syntax
		if len(stack.Files) > 0 && stack.Root != "" {
			if _, err := client.ComposeConfigFull(ctx, stack.Root, stack.Files, stack.Profiles, []string{}, []string{}); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return apperr.Wrap("validator.ValidateDaemon", apperr.External, err, "invalid compose file for stack %s", stackKey)
			}
		}
	}

	return nil
}
