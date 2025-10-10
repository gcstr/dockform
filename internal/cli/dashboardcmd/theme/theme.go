package theme

import "github.com/charmbracelet/lipgloss/v2"

// Foreground colors
var (
	FgBase      = lipgloss.Color("#C8D3F5")
	FgHalfMuted = lipgloss.Color("#828BB8")
	FgMuted     = lipgloss.Color("#444A73")
	FgSubtle    = lipgloss.Color("#313657")
	FgSelected  = lipgloss.Color("#F1EFEF")
)

// Background colors
var (
	BgBase = lipgloss.Color("#222436")
)

// Status colors
var (
	Success = lipgloss.Color("#12C78F")
	Error   = lipgloss.Color("#EB4268")
	Warning = lipgloss.Color("#E8FE96")
	Info    = lipgloss.Color("#00A4FF")
)

// Colors
const (
	GradientStartHex = "#5EC6F6"
	GradientEndHex   = "#376FE9"
)

var (
	Primary   = lipgloss.Color("#5EC6F6")
	Secondary = lipgloss.Color("#FF60FF")
	Tertiary  = lipgloss.Color("#68FFD6")
	Accent    = lipgloss.Color("#E8FE96")

	GradientStart = lipgloss.Color(GradientStartHex)
	GradientEnd   = lipgloss.Color(GradientEndHex)
)
