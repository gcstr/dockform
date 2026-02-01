package dashboardcmd

import (
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/buildinfo"
)

// displayVersion returns a clean version string.
func displayVersion(version string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		return buildinfo.Version()
	}
	return v
}

// displayIdentifier returns a clean identifier or placeholder.
func displayIdentifier(identifier string) string {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return "(unset)"
	}
	return id
}

// displayContextName returns a clean context name or default.
func displayContextName(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		return "default"
	}
	return n
}

// displayDockerHost returns a clean docker host or placeholder.
func displayDockerHost(host string) string {
	h := strings.TrimSpace(host)
	if h == "" {
		return "(unknown)"
	}
	return h
}

// displayEngineVersion returns a clean engine version or placeholder.
func displayEngineVersion(version string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		return "(unknown)"
	}
	return v
}

// displayManifestPath returns a clean manifest path or placeholder.
func displayManifestPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return "(unknown)"
	}
	return p
}

// displayVolumeMount returns a clean volume mountpoint or placeholder.
func displayVolumeMount(mount string) string {
	m := strings.TrimSpace(mount)
	if m == "" {
		return "(no mountpoint)"
	}
	return m
}

// displayVolumeDriver returns a clean volume driver or placeholder.
func displayVolumeDriver(driver string) string {
	d := strings.TrimSpace(driver)
	if d == "" {
		return "(driver unknown)"
	}
	return d
}

// displayNetworkDriver returns a clean network driver or placeholder.
func displayNetworkDriver(driver string) string {
	d := strings.TrimSpace(driver)
	if d == "" {
		return "(driver unknown)"
	}
	return d
}

// truncateRight truncates a string from the right with ellipsis if it exceeds width.
func truncateRight(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	ellipsis := "..."
	ellipsisWidth := lipgloss.Width(ellipsis)
	if width <= ellipsisWidth {
		return ellipsis[:min(width, len(ellipsis))]
	}
	target := width - ellipsisWidth
	var builder strings.Builder
	current := 0
	for _, r := range value {
		rw := lipgloss.Width(string(r))
		if current+rw > target {
			break
		}
		builder.WriteRune(r)
		current += rw
	}
	return builder.String() + ellipsis
}

// truncateLeft truncates a string from the left with ellipsis if it exceeds width.
func truncateLeft(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	ellipsis := "..."
	ellipsisWidth := lipgloss.Width(ellipsis)
	if width <= ellipsisWidth {
		return ellipsis[:min(width, len(ellipsis))]
	}
	target := width - ellipsisWidth
	runes := []rune(value)
	current := 0
	start := len(runes)
	for start > 0 && current < target {
		start--
		rw := lipgloss.Width(string(runes[start]))
		if current+rw > target {
			break
		}
		current += rw
	}
	return ellipsis + string(runes[start:])
}

// availableValueWidth computes the available width for a value given a total width and key.
func availableValueWidth(totalWidth int, key string) int {
	width := totalWidth - lipgloss.Width(key+": ")
	if width < 0 {
		return 0
	}
	return width
}
