package components

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss/v2"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func TestRenderHeaderFitsWidth(t *testing.T) {
	const (
		title   = "Stacks"
		width   = 40
		padding = 2
	)

	got := RenderHeader(title, width, padding)
	contentWidth := width - padding
	if w := lipgloss.Width(got); w != contentWidth {
		t.Fatalf("expected width %d, got %d", contentWidth, w)
	}
	plain := stripANSI(got)
	if !strings.HasPrefix(plain, "◇ "+title) {
		t.Fatalf("expected header to start with title, got %q", plain)
	}
	if !strings.Contains(plain, patternChar) {
		t.Fatalf("expected filler pattern %q in %q", patternChar, plain)
	}
}

func TestRenderHeaderTruncatesLongTitle(t *testing.T) {
	got := RenderHeader("Super long title that surely exceeds the width", 12, 2)
	if got == "" {
		t.Fatalf("expected truncated header, got empty string")
	}
	if w := lipgloss.Width(got); w != 10 {
		t.Fatalf("expected truncated width 10, got %d", w)
	}
}

func TestRenderHeaderActiveDiffers(t *testing.T) {
	inactive := RenderHeader("Stacks", 26, 2)
	active := RenderHeaderActive("Stacks", 26, 2)
	if inactive == active {
		t.Fatalf("expected active header to differ from inactive one")
	}
}

func TestRenderSimple(t *testing.T) {
	got := RenderSimple("Context", "default")
	plain := stripANSI(got)
	if plain != "Context: default" {
		t.Fatalf("unexpected plain output %q", plain)
	}
}

func TestRenderVolumeStructure(t *testing.T) {
	got := RenderVolume("vault", "/mnt/data", "1.2GB")
	plain := stripANSI(got)
	lines := strings.Split(plain, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "vault") {
		t.Fatalf("first line should contain name, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "├ ") || !strings.Contains(lines[1], "/mnt/data") {
		t.Fatalf("second line unexpected: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "└ ") || !strings.Contains(lines[2], "1.2GB") {
		t.Fatalf("third line unexpected: %q", lines[2])
	}
}

func TestRenderNetwork(t *testing.T) {
	got := RenderNetwork("frontend", "bridge")
	plain := stripANSI(got)
	if plain != "frontend - bridge" {
		t.Fatalf("unexpected plain network output %q", plain)
	}
}

func TestLogsPagerLifecycle(t *testing.T) {
	p := NewLogsPager()
	if view := p.View(); view != "" {
		t.Fatalf("expected empty view before size, got %q", view)
	}

	p.SetSize(20, 5)
	p.SetContent("hello world")
	if view := p.View(); !strings.Contains(view, "hello world") {
		t.Fatalf("expected view to contain content, got %q", view)
	}
}

func TestStackItemFilterValue(t *testing.T) {
	item := StackItem{TitleText: "paperless"}
	if got := item.FilterValue(); got != "paperless" {
		t.Fatalf("expected default filter value 'paperless', got %q", got)
	}
	item.FilterText = "custom filter"
	if got := item.FilterValue(); got != "custom filter" {
		t.Fatalf("expected custom filter value, got %q", got)
	}
}
