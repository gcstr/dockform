package manifest

import (
	"path/filepath"
	"strings"

	"github.com/gcstr/dockform/internal/apperr"
)

func (c *Config) normalizeAndValidate(baseDir string) error {
	// Defaults
	if c.Docker.Context == "" {
		c.Docker.Context = "default"
	}
	// Require docker.identifier
	if strings.TrimSpace(c.Docker.Identifier) == "" {
		return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "docker.identifier is required")
	}
	if c.Applications == nil {
		c.Applications = map[string]Application{}
	}
	if c.Networks == nil {
		c.Networks = map[string]TopLevelResourceSpec{}
	}
	if c.Filesets == nil {
		c.Filesets = map[string]FilesetSpec{}
	}

	// Validate application keys and fill defaults
	for appName, app := range c.Applications {
		if !appKeyRegex.MatchString(appName) {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "invalid application key %q: must match ^[a-z0-9_.-]+$", appName)
		}
		// Resolve root relative to config file directory
		resolvedRoot := filepath.Clean(filepath.Join(baseDir, app.Root))

		// Merge environment files with correct base paths
		// Root-level files are converted from baseDir-relative to resolvedRoot-relative
		var mergedEnv []string
		if c.Environment != nil && len(c.Environment.Files) > 0 {
			mergedEnv = append(mergedEnv, rebaseRootEnvToApp(baseDir, resolvedRoot, c.Environment.Files)...)
		}
		if app.Environment != nil && len(app.Environment.Files) > 0 {
			mergedEnv = append(mergedEnv, app.Environment.Files...)
		}
		if len(app.EnvFile) > 0 {
			mergedEnv = append(mergedEnv, app.EnvFile...)
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

		// Merge inline env vars (root first, then app). Last value for a key wins.
		var mergedInline []string
		if c.Environment != nil && len(c.Environment.Inline) > 0 {
			mergedInline = append(mergedInline, c.Environment.Inline...)
		}
		if app.Environment != nil && len(app.Environment.Inline) > 0 {
			mergedInline = append(mergedInline, app.Environment.Inline...)
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

		// Merge SOPS secrets: root-level rebased to app root, then app-level
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
		if app.Secrets != nil && len(app.Secrets.Sops) > 0 {
			for _, sp := range app.Secrets.Sops {
				p := strings.TrimSpace(sp)
				if p == "" {
					continue
				}
				if !strings.HasSuffix(strings.ToLower(p), ".env") {
					return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "application %s secrets.sops: %s must have .env extension", appName, sp)
				}
				mergedSops = append(mergedSops, p)
			}
		}

		if len(app.Files) == 0 {
			c.Applications[appName] = Application{
				Root:        resolvedRoot,
				Files:       []string{filepath.Join(resolvedRoot, "docker-compose.yml")},
				Profiles:    app.Profiles,
				EnvFile:     mergedEnv,
				Environment: app.Environment,
				Secrets:     app.Secrets,
				EnvInline:   mergedInline,
				SopsSecrets: mergedSops,
				Project:     app.Project,
			}
		} else {
			// Keep provided file paths (interpreted relative to Root by compose), but store resolved Root
			c.Applications[appName] = Application{
				Root:        resolvedRoot,
				Files:       app.Files,
				Profiles:    app.Profiles,
				EnvFile:     mergedEnv,
				Environment: app.Environment,
				Secrets:     app.Secrets,
				EnvInline:   mergedInline,
				SopsSecrets: mergedSops,
				Project:     app.Project,
			}
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
		if a.TargetPath == "" || !filepath.IsAbs(a.TargetPath) {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "fileset %s: target_path must be an absolute path", filesetName)
		}
		if a.TargetPath == "/" {
			return apperr.New("manifest.normalizeAndValidate", apperr.InvalidInput, "fileset %s: target_path cannot be '/'", filesetName)
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

func rebaseRootEnvToApp(baseDir, resolvedRoot string, files []string) []string {
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
