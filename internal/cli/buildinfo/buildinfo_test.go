package buildinfo

import "testing"

func withBuildVars(versionVal, commitVal, dateVal, builtByVal, goVal string, fn func()) {
	oldVersion, oldCommit, oldDate, oldBuiltBy, oldGo := version, commit, date, builtBy, goVersion
	version, commit, date, builtBy, goVersion = versionVal, commitVal, dateVal, builtByVal, goVal
	defer func() { version, commit, date, builtBy, goVersion = oldVersion, oldCommit, oldDate, oldBuiltBy, oldGo }()
	fn()
}

func TestVersionSimpleIncludesShortCommit(t *testing.T) {
	withBuildVars("1.2.3", "abcdef1234", "", "", "go1.22", func() {
		if got := VersionSimple(); got != "1.2.3 (abcdef1)" {
			t.Fatalf("expected abbreviated commit in VersionSimple, got %q", got)
		}
	})
}

func TestVersionDetailedWithMetadata(t *testing.T) {
	withBuildVars("2.0.0", "abc", "2024-04-01", "builder", "go1.22", func() {
		got := VersionDetailed()
		if got != "2.0.0 (abc, 2024-04-01, builder)" {
			t.Fatalf("unexpected detailed version: %q", got)
		}
		if GoVersion() != "go1.22" {
			t.Fatalf("expected go version passthrough")
		}
	})
}
