package components

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
)

// RenderGradientText colors each character of text with a gradient
// transitioning from startHex to endHex (e.g., "#5EC6F6" to "#376FE9").
// The gradient spans the full rune-length of the input text.
func RenderGradientText(text string, startHex string, endHex string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}

	sr, sg, sb := hexToRGB(startHex)
	er, eg, eb := hexToRGB(endHex)

	var b strings.Builder
	// Heuristic capacity: each rune + ANSI codes; keep it modest
	b.Grow(len(text) * 10)

	den := float64(maxInt(1, len(runes)-1))
	for i, r := range runes {
		// Position in gradient [0,1]
		t := float64(i) / den
		rr := uint8(math.Round((1-t)*float64(sr) + t*float64(er)))
		gg := uint8(math.Round((1-t)*float64(sg) + t*float64(eg)))
		bb := uint8(math.Round((1-t)*float64(sb) + t*float64(eb)))
		col := lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", rr, gg, bb))
		b.WriteString(lipgloss.NewStyle().Foreground(col).Render(string(r)))
	}
	return b.String()
}

func hexToRGB(hex string) (int, int, int) {
	// Normalize leading '#'
	hex = strings.TrimPrefix(hex, "#")
	// Only support 6-digit hex; fallback to white if invalid
	if len(hex) != 6 {
		return 255, 255, 255
	}
	var r, g, b int
	_, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	if err != nil {
		return 255, 255, 255
	}
	return r, g, b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
