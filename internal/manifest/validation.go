package manifest

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
)

// findDefaultComposeFile looks for compose files in the given directory, selecting in order:
// compose.yaml > compose.yml > docker-compose.yaml > docker-compose.yml. If none exist,
// it defaults to compose.yaml under the provided directory.
func findDefaultComposeFile(dir string) string {
	candidates := []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}
	for _, candidate := range candidates {
		fullPath := filepath.Join(dir, candidate)
		if _, err := os.Stat(fullPath); err == nil {
			return fullPath
		}
	}
	// If none exist, default to compose.yaml for modern Compose behavior
	return filepath.Join(dir, "compose.yaml")
}

func (c *Config) normalizeAndValidate(baseDir string) error {
	// Initialize maps if nil
	if c.Contexts == nil {
		c.Contexts = map[string]ContextConfig{}
	}
	if c.Deployments == nil {
		c.Deployments = map[string]DeploymentConfig{}
	}
	if c.Stacks == nil {
		c.Stacks = map[string]Stack{}
	}
	if c.DiscoveredStacks == nil {
		c.DiscoveredStacks = map[string]Stack{}
	}
	if c.DiscoveredFilesets == nil {
		c.DiscoveredFilesets = map[string]FilesetSpec{}
	}

	// Require identifier
	if strings.TrimSpace(c.Identifier) == "" {
		return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "identifier is required at the top level of the manifest")
	}

	// Require at least one context
	if len(c.Contexts) == 0 {
		return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "at least one context must be defined under 'contexts:'")
	}

	// Validate context configurations
	for contextName := range c.Contexts {
		if !contextKeyRegex.MatchString(contextName) {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "invalid context key %q: must match ^[a-z0-9_-]+$", contextName)
		}
	}

	// Validate deployment groups
	for deployName, deploy := range c.Deployments {
		// Validate referenced contexts exist
		for _, ctxName := range deploy.Contexts {
			if _, ok := c.Contexts[ctxName]; !ok {
				return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "deployment %s: references unknown context %q", deployName, ctxName)
			}
		}
		// Validate referenced stacks format (context/stack)
		for _, stackKey := range deploy.Stacks {
			context, _, err := ParseStackKey(stackKey)
			if err != nil {
				return apperr.Wrap("manifest.normalizeAndValidate", apperr.InvalidInput, err, "deployment %s: invalid stack reference", deployName)
			}
			if _, ok := c.Contexts[context]; !ok {
				return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "deployment %s: stack %q references unknown context %q", deployName, stackKey, context)
			}
		}
	}

	// Validate and normalize explicit stack overrides
	for stackKey, stack := range c.Stacks {
		context, stackName, err := ParseStackKey(stackKey)
		if err != nil {
			return apperr.Wrap("manifest.normalizeAndValidate", apperr.InvalidInput, err, "invalid stack key")
		}

		// Validate context exists
		if _, ok := c.Contexts[context]; !ok {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "stack %s: references unknown context %q", stackKey, context)
		}

		// Validate stack name format
		if !appKeyRegex.MatchString(stackName) {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "invalid stack name %q in key %q: must match ^[a-z0-9_.-]+$", stackName, stackKey)
		}

		// Set the context reference
		stack.Context = context
		c.Stacks[stackKey] = stack
	}

	// Normalize all stacks (discovered + explicit merged)
	allStacks := c.GetAllStacks()
	for stackKey, stack := range allStacks {
		context, _, err := ParseStackKey(stackKey)
		if err != nil {
			continue // Skip invalid keys (shouldn't happen)
		}

		// Ensure context is set
		if stack.Context == "" {
			stack.Context = context
		}

		// Resolve root if not absolute
		if stack.Root != "" && !filepath.IsAbs(stack.Root) {
			stack.Root = filepath.Clean(filepath.Join(baseDir, stack.Root))
		}
		stack.RootAbs = stack.Root

		// Normalize compose files
		if len(stack.Files) == 0 && stack.Root != "" {
			defaultComposeFile := findDefaultComposeFile(stack.Root)
			stack.Files = []string{defaultComposeFile}
		}

		// Merge inline env vars
		var mergedInline []string
		if stack.Environment != nil && len(stack.Environment.Inline) > 0 {
			mergedInline = append(mergedInline, stack.Environment.Inline...)
		}
		if len(mergedInline) > 1 {
			// Deduplicate by key with last-wins while preserving order of last occurrences
			seen := map[string]struct{}{}
			dedupReversed := make([]string, 0, len(mergedInline))
			for i := len(mergedInline) - 1; i >= 0; i-- {
				kv := mergedInline[i]
				if kv == "" {
					continue
				}
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) == 0 || parts[0] == "" {
					continue
				}
				key := parts[0]
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				dedupReversed = append(dedupReversed, kv)
			}
			mergedInline = make([]string, 0, len(dedupReversed))
			for i := len(dedupReversed) - 1; i >= 0; i-- {
				mergedInline = append(mergedInline, dedupReversed[i])
			}
		}
		stack.EnvInline = mergedInline

		// Merge SOPS secrets from YAML Secrets.Sops into computed SopsSecrets
		if stack.Secrets != nil && len(stack.Secrets.Sops) > 0 {
			stack.SopsSecrets = append(stack.SopsSecrets, stack.Secrets.Sops...)
		}

		// Validate SOPS secrets have .env extension
		for _, sp := range stack.SopsSecrets {
			if !strings.HasSuffix(strings.ToLower(sp), ".env") {
				return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "stack %s: secrets file %s must have .env extension", stackKey, sp)
			}
		}

		// Validate bind mounts - check for relative path bind mounts that won't work with remote contexts
		if err := validateBindMountsInComposeFile(stackKey, stack); err != nil {
			return err
		}

		// Update the stack in discovered (which will be merged in GetAllStacks)
		if _, isDiscovered := c.DiscoveredStacks[stackKey]; isDiscovered {
			c.DiscoveredStacks[stackKey] = stack
		} else {
			c.Stacks[stackKey] = stack
		}
	}

	// Validate SOPS config (global)
	if c.Sops != nil {
		// Migration error: top-level recipients deprecated
		if len(c.Sops.Recipients) > 0 {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "sops.recipients is no longer supported; move entries under sops.age.recipients or sops.pgp.recipients")
		}
		// Validate age
		if c.Sops.Age != nil {
			// key_file optional; if set, leave as-is (resolved at runtime for ~)
			// recipients format: if provided, must start with age1
			for _, r := range c.Sops.Age.Recipients {
				v := strings.TrimSpace(r)
				if v == "" {
					continue
				}
				if !strings.HasPrefix(v, "age1") {
					return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "sops.age.recipients: invalid age recipient format: %s", r)
				}
			}
		}
		// Validate pgp
		if c.Sops.Pgp != nil {
			mode := strings.TrimSpace(strings.ToLower(c.Sops.Pgp.PinentryMode))
			if mode == "" {
				mode = "default"
			}
			if mode != "default" && mode != "loopback" {
				return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "sops.pgp.pinentry_mode must be 'default' or 'loopback'")
			}
			c.Sops.Pgp.PinentryMode = mode
		}
	}

	// Validate and normalize discovered filesets
	for filesetKey, fs := range c.DiscoveredFilesets {
		// Validate source
		if strings.TrimSpace(fs.Source) == "" {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "fileset %s: source path is required", filesetKey)
		}
		if fs.TargetVolume == "" {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "fileset %s: target_volume is required", filesetKey)
		}

		// target_path must be an absolute Unix path since it's used inside containers
		// For discovered filesets, default to "/" if not set
		if fs.TargetPath == "" {
			fs.TargetPath = "/"
		}
		if !strings.HasPrefix(fs.TargetPath, "/") {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "fileset %s: target_path must be an absolute path", filesetKey)
		}

		// apply_mode: default to hot, validate values
		mode := strings.ToLower(strings.TrimSpace(fs.ApplyMode))
		if mode == "" {
			mode = "hot"
		}
		if mode != "hot" && mode != "cold" {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "fileset %s: apply_mode must be 'hot' or 'cold'", filesetKey)
		}
		fs.ApplyMode = mode

		// Validate and normalize ownership if provided
		if err := validateOwnership(filesetKey, &fs); err != nil {
			return err
		}

		// Resolve source to absolute path if needed
		if !filepath.IsAbs(fs.Source) {
			fs.SourceAbs = filepath.Clean(filepath.Join(baseDir, fs.Source))
		} else {
			fs.SourceAbs = fs.Source
		}

		c.DiscoveredFilesets[filesetKey] = fs
	}

	return nil
}

// Regex patterns for validation
var (
	numericIDRegex = regexp.MustCompile(`^\d+$`)
	posixNameRegex = regexp.MustCompile(`^[a-z_][a-z0-9_-]*\$?$`)
)

// validateOwnership validates and normalizes ownership settings for a fileset.
// It trims whitespace from all string fields and persists the normalized values.
func validateOwnership(filesetName string, fs *FilesetSpec) error {
	if fs == nil || fs.Ownership == nil {
		return nil
	}

	o := fs.Ownership

	// Validate and normalize user if provided
	if o.User != "" {
		trimmed := strings.TrimSpace(o.User)
		if trimmed == "" {
			return apperr.New("manifest.validateOwnership", apperr.InvalidInput, "fileset %s: user cannot be empty or whitespace only", filesetName)
		}
		_, err := validateUserOrGroup(trimmed)
		if err != nil {
			return apperr.Wrap("manifest.validateOwnership", apperr.InvalidInput, err, "fileset %s: invalid user", filesetName)
		}
		o.User = trimmed // Persist trimmed value
	}

	// Validate and normalize group if provided
	if o.Group != "" {
		trimmed := strings.TrimSpace(o.Group)
		if trimmed == "" {
			return apperr.New("manifest.validateOwnership", apperr.InvalidInput, "fileset %s: group cannot be empty or whitespace only", filesetName)
		}
		_, err := validateUserOrGroup(trimmed)
		if err != nil {
			return apperr.Wrap("manifest.validateOwnership", apperr.InvalidInput, err, "fileset %s: invalid group", filesetName)
		}
		o.Group = trimmed // Persist trimmed value
	}

	// Validate and normalize file_mode if provided
	if o.FileMode != "" {
		trimmed := strings.TrimSpace(o.FileMode)
		if trimmed == "" {
			return apperr.New("manifest.validateOwnership", apperr.InvalidInput, "fileset %s: file_mode cannot be empty or whitespace only", filesetName)
		}
		if _, err := parseOctalMode(trimmed); err != nil {
			return apperr.Wrap("manifest.validateOwnership", apperr.InvalidInput, err, "fileset %s: invalid file_mode", filesetName)
		}
		o.FileMode = trimmed // Persist trimmed value
	}

	// Validate and normalize dir_mode if provided
	if o.DirMode != "" {
		trimmed := strings.TrimSpace(o.DirMode)
		if trimmed == "" {
			return apperr.New("manifest.validateOwnership", apperr.InvalidInput, "fileset %s: dir_mode cannot be empty or whitespace only", filesetName)
		}
		if _, err := parseOctalMode(trimmed); err != nil {
			return apperr.Wrap("manifest.validateOwnership", apperr.InvalidInput, err, "fileset %s: invalid dir_mode", filesetName)
		}
		o.DirMode = trimmed // Persist trimmed value
	}

	return nil
}

// validateUserOrGroup validates a user or group identifier.
// Returns (isNumeric, error).
func validateUserOrGroup(s string) (bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return false, apperr.New("manifest.validateUserOrGroup", apperr.InvalidInput, "user or group cannot be empty")
	}

	// Check if numeric
	if numericIDRegex.MatchString(s) {
		return true, nil
	}

	// Check if valid POSIX name (case-insensitive)
	if posixNameRegex.MatchString(strings.ToLower(s)) {
		return false, nil
	}

	return false, apperr.New("manifest.validateUserOrGroup", apperr.InvalidInput, "must be numeric or valid POSIX name: %s", s)
}

// parseOctalMode parses an octal mode string (e.g., "0644" or "644") and returns the numeric value.
func parseOctalMode(s string) (uint32, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, apperr.New("manifest.parseOctalMode", apperr.InvalidInput, "mode cannot be empty")
	}

	// Check original length: must be 3 or 4 characters (e.g., "644", "0644", "755", "4755")
	if len(s) < 3 || len(s) > 4 {
		return 0, apperr.New("manifest.parseOctalMode", apperr.InvalidInput, "mode must be 3-4 octal digits: %s", s)
	}

	// Validate all characters are octal digits
	for _, c := range s {
		if c < '0' || c > '7' {
			return 0, apperr.New("manifest.parseOctalMode", apperr.InvalidInput, "mode must contain only octal digits (0-7): %s", s)
		}
	}

	// Parse as octal (strip leading 0 for parsing)
	normalized := strings.TrimPrefix(s, "0")
	val, err := strconv.ParseUint(normalized, 8, 32)
	if err != nil {
		return 0, apperr.Wrap("manifest.parseOctalMode", apperr.InvalidInput, err, "invalid octal mode: %s", s)
	}

	return uint32(val), nil
}
