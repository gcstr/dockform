package util

import (
	"path/filepath"
	"strings"
)

func SplitNonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// DirPath returns the directory portion of a path (similar to filepath.Dir but handles slash-separated paths).
func DirPath(p string) string {
	if p == "" || p == "." || p == "/" {
		return ""
	}
	dir := filepath.Dir(p)
	if dir == "." {
		return ""
	}
	return dir
}

// ShellEscape escapes a string for safe use in a shell script within single quotes.
// It replaces any single quotes with '\'' (end quote, escaped quote, start quote).
// Usage: sh -c "echo '" + ShellEscape(userInput) + "'"
func ShellEscape(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}
