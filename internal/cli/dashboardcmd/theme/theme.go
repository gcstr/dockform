package theme

import "github.com/charmbracelet/lipgloss/v2"

// Foreground colors
var (
	FgBase      = lipgloss.Color("#DFDBDD")
	FgMuted     = lipgloss.Color("#858392")
	FgHalfMuted = lipgloss.Color("#BFBCC8")
	FgSubtle    = lipgloss.Color("#605F6B")
	FgSelected  = lipgloss.Color("#F1EFEF")
)

// Background colors
var (
	BgBase = lipgloss.Color("#201F26")
)

// Status colors
var (
	Success = lipgloss.Color("#12C78F")
	Error   = lipgloss.Color("#EB4268")
	Warning = lipgloss.Color("#E8FE96")
	Info    = lipgloss.Color("#00A4FF")
)

// Brand colors
var (
	Primary   = lipgloss.Color("#6B50FF")
	Secondary = lipgloss.Color("#FF60FF")
	Tertiary  = lipgloss.Color("#68FFD6")
	Accent    = lipgloss.Color("#E8FE96")
)
