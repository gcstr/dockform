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
	// Defaults
	if c.Docker.Context == "" {
		c.Docker.Context = "default"
	}
	// Require docker.identifier
	if strings.TrimSpace(c.Docker.Identifier) == "" {
		return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "docker.identifier is required")
	}
	if c.Stacks == nil {
		c.Stacks = map[string]Stack{}
	}
	if c.Networks == nil {
		c.Networks = map[string]NetworkSpec{}
	}
	if c.Volumes == nil {
		c.Volumes = map[string]TopLevelResourceSpec{}
	}
	if c.Filesets == nil {
		c.Filesets = map[string]FilesetSpec{}
	}

	// Validate volume names
	for volumeName := range c.Volumes {
		if !appKeyRegex.MatchString(volumeName) {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "invalid volume key %q: must match ^[a-z0-9_.-]+$", volumeName)
		}
	}

	// Validate network names
	for networkName := range c.Networks {
		if !appKeyRegex.MatchString(networkName) {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "invalid network key %q: must match ^[a-z0-9_.-]+$", networkName)
		}
	}

	// Validate stack keys and fill defaults
	for stackName, stack := range c.Stacks {
		if !appKeyRegex.MatchString(stackName) {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "invalid stack key %q: must match ^[a-z0-9_.-]+$", stackName)
		}
		// Resolve root relative to config file directory
		resolvedRoot := filepath.Clean(filepath.Join(baseDir, stack.Root))

		// Merge environment files with correct base paths
		// Root-level files are converted from baseDir-relative to resolvedRoot-relative
		var mergedEnv []string
		if c.Environment != nil && len(c.Environment.Files) > 0 {
			mergedEnv = append(mergedEnv, rebaseRootEnvToStack(baseDir, resolvedRoot, c.Environment.Files)...)
		}
		if stack.Environment != nil && len(stack.Environment.Files) > 0 {
			mergedEnv = append(mergedEnv, stack.Environment.Files...)
		}
		if len(stack.EnvFile) > 0 {
			mergedEnv = append(mergedEnv, stack.EnvFile...)
		}
		// De-duplicate env files while preserving order
		if len(mergedEnv) > 1 {
			seen := make(map[string]struct{}, len(mergedEnv))
			uniq := make([]string, 0, len(mergedEnv))
			for _, p := range mergedEnv {
				if _, ok := seen[p]; ok {
					continue
				}
				seen[p] = struct{}{}
				uniq = append(uniq, p)
			}
			mergedEnv = uniq
		}

		// Merge inline env vars (root first, then stack). Last value for a key wins.
		var mergedInline []string
		if c.Environment != nil && len(c.Environment.Inline) > 0 {
			mergedInline = append(mergedInline, c.Environment.Inline...)
		}
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

		// Merge SOPS secrets: root-level rebased to stack root, then stack-level
		var mergedSops []string
		if c.Secrets != nil && len(c.Secrets.Sops) > 0 {
			for _, sp := range c.Secrets.Sops {
				p := strings.TrimSpace(sp)
				if p == "" {
					continue
				}
				if !strings.HasSuffix(strings.ToLower(p), ".env") {
					return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "secrets.sops: %s must have .env extension", sp)
				}
				abs := filepath.Clean(filepath.Join(baseDir, p))
				if rel, err := filepath.Rel(resolvedRoot, abs); err == nil {
					mergedSops = append(mergedSops, rel)
				} else {
					mergedSops = append(mergedSops, abs)
				}
			}
		}
		if stack.Secrets != nil && len(stack.Secrets.Sops) > 0 {
			for _, sp := range stack.Secrets.Sops {
				p := strings.TrimSpace(sp)
				if p == "" {
					continue
				}
				if !strings.HasSuffix(strings.ToLower(p), ".env") {
					return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "stack %s secrets.sops: %s must have .env extension", stackName, sp)
				}
				mergedSops = append(mergedSops, p)
			}
		}

		if len(stack.Files) == 0 {
			defaultComposeFile := findDefaultComposeFile(resolvedRoot)
			c.Stacks[stackName] = Stack{
				Root:        resolvedRoot,
				Files:       []string{defaultComposeFile},
				Profiles:    stack.Profiles,
				EnvFile:     mergedEnv,
				Environment: stack.Environment,
				Secrets:     stack.Secrets,
				EnvInline:   mergedInline,
				SopsSecrets: mergedSops,
				Project:     stack.Project,
			}
		} else {
			// Keep provided file paths (interpreted relative to Root by compose), but store resolved Root
			c.Stacks[stackName] = Stack{
				Root:        resolvedRoot,
				Files:       stack.Files,
				Profiles:    stack.Profiles,
				EnvFile:     mergedEnv,
				Environment: stack.Environment,
				Secrets:     stack.Secrets,
				EnvInline:   mergedInline,
				SopsSecrets: mergedSops,
				Project:     stack.Project,
			}
		}
	}

	// Validate SOPS config (global). Stacks inherit usage only; config is global
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
			// No strict validation for recipients (fingerprint/email)
		}
	}

	// Filesets validation and normalization
	for filesetName, a := range c.Filesets {
		if !appKeyRegex.MatchString(filesetName) {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "invalid fileset key %q: must match ^[a-z0-9_.-]+$", filesetName)
		}
		if strings.TrimSpace(a.Source) == "" {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "fileset %s: source path is required", filesetName)
		}
		if a.TargetVolume == "" {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "fileset %s: target_volume is required", filesetName)
		}
		// target_path must be an absolute Unix path since it's used inside containers
		if a.TargetPath == "" || !strings.HasPrefix(a.TargetPath, "/") {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "fileset %s: target_path must be an absolute path", filesetName)
		}
		if a.TargetPath == "/" {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "fileset %s: target_path cannot be '/'", filesetName)
		}
		// apply_mode: default to hot, validate values
		mode := strings.ToLower(strings.TrimSpace(a.ApplyMode))
		if mode == "" {
			mode = "hot"
		}
		if mode != "hot" && mode != "cold" {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "fileset %s: apply_mode must be 'hot' or 'cold'", filesetName)
		}
		a.ApplyMode = mode

		// Validate and normalize ownership if provided
		if err := validateOwnership(filesetName, &a); err != nil {
			return err
		}

		// Resolve source relative to baseDir
		srcAbs := a.Source
		if !filepath.IsAbs(srcAbs) {
			srcAbs = filepath.Clean(filepath.Join(baseDir, srcAbs))
		}
		a.SourceAbs = srcAbs
		c.Filesets[filesetName] = a
	}

	return nil
}

func rebaseRootEnvToStack(baseDir, resolvedRoot string, files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if f == "" {
			continue
		}
		abs := filepath.Clean(filepath.Join(baseDir, f))
		if rel, err := filepath.Rel(resolvedRoot, abs); err == nil {
			out = append(out, rel)
		} else {
			out = append(out, abs)
		}
	}
	return out
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
		// Note: non-numeric IDs are allowed but may not be portable across helper images
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
		// Note: non-numeric IDs are allowed but may not be portable across helper images
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
