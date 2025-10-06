package buildinfo

// Build-time variables injected via -ldflags; defaults are used for dev builds.
var (
	version   = "0.1.0-dev"
	commit    = ""
	date      = ""
	builtBy   = ""
	goVersion = ""
)

// Version returns the semantic version string.
func Version() string {
	return version
}

// VersionSimple returns version number with short commit hash for --version flag.
func VersionSimple() string {
	v := version
	if commit != "" {
		if len(commit) >= 7 {
			v += " (" + commit[:7] + ")"
		} else {
			v += " (" + commit + ")"
		}
	}
	return v
}

// VersionDetailed returns version info with build metadata if available.
func VersionDetailed() string {
	v := version
	if commit != "" {
		v += " (" + commit
		if date != "" {
			v += ", " + date
		}
		if builtBy != "" {
			v += ", " + builtBy
		}
		v += ")"
	}
	return v
}

// GoVersion returns the build-time Go version if provided.
func GoVersion() string { return goVersion }

// Commit returns the full commit hash if provided via -ldflags.
func Commit() string { return commit }

// BuildDate returns the build date if provided via -ldflags.
func BuildDate() string { return date }

// BuiltBy returns the builder identifier if provided via -ldflags.
func BuiltBy() string { return builtBy }
