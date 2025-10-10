package components

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
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

	got := RenderHeader(title, width, padding, "slash")
	contentWidth := width - padding
	if w := lipgloss.Width(got); w != contentWidth {
		t.Fatalf("expected width %d, got %d", contentWidth, w)
	}
	plain := stripANSI(got)
	if !strings.HasPrefix(plain, "◇ "+title) {
		t.Fatalf("expected header to start with title, got %q", plain)
	}
	if !strings.Contains(plain, "╱") {
		t.Fatalf("expected filler pattern %q in %q", "╱", plain)
	}
}

func TestRenderHeaderTruncatesLongTitle(t *testing.T) {
	got := RenderHeader("Super long title that surely exceeds the width", 12, 2, "slash")
	if got == "" {
		t.Fatalf("expected truncated header, got empty string")
	}
	if w := lipgloss.Width(got); w != 10 {
		t.Fatalf("expected truncated width 10, got %d", w)
	}
}

func TestRenderHeaderActiveDiffers(t *testing.T) {
	inactive := RenderHeader("Stacks", 26, 2, "slash")
	active := RenderHeaderActive("Stacks", 26, 2, "slash")
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
	got := RenderVolume("vault", "/mnt/data", "1.2GB", false)
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
	got := RenderNetwork("frontend", "bridge", false)
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

func TestLogsPagerUpdateNoPanic(t *testing.T) {
	p := NewLogsPager()
	p.SetSize(10, 3)
	var msg tea.Msg
	p, _ = p.Update(msg)
	_ = p // ensure p is used
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

func TestStackItemRendersWithStatusKind(t *testing.T) {
	m := list.New([]list.Item{}, StacksDelegate{}, 40, 10)
	item := StackItem{TitleText: "app", Containers: []string{"web", "nginx:alpine"}, Status: "Up 2m (healthy)", StatusKind: "success"}
	var buf strings.Builder
	StacksDelegate{}.Render(&buf, m, 0, item)
	plain := stripANSI(buf.String())
	if !strings.Contains(plain, "Up 2m (healthy)") {
		t.Fatalf("expected status text to be present, got %q", plain)
	}
	if !strings.Contains(plain, "● Up 2m (healthy)") {
		t.Fatalf("expected bullet then status text, got %q", plain)
	}
}
