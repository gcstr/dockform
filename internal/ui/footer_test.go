package ui

import (
	"strings"
	"testing"
)

func TestRenderNestedSections_Footer(t *testing.T) {
	sections := []NestedSection{
		{
			Title: "Volumes",
			Items: []DiffLine{
				{Type: Add, Message: "netbird_data will be created"},
			},
			Footer: []DiffLine{
				{Type: Info, Message: "30 unchanged"},
			},
		},
	}

	out := StripANSI(RenderNestedSections(sections))

	if !strings.Contains(out, "netbird_data will be created") {
		t.Errorf("expected output to contain item message, got:\n%s", out)
	}
	if !strings.Contains(out, "30 unchanged") {
		t.Errorf("expected output to contain footer message, got:\n%s", out)
	}

	itemIdx := strings.Index(out, "netbird_data will be created")
	footerIdx := strings.Index(out, "30 unchanged")
	if footerIdx <= itemIdx {
		t.Errorf("expected footer to appear after items, but item index=%d footer index=%d", itemIdx, footerIdx)
	}

	// Find the line containing "30 unchanged" and assert exact format: two leading spaces, no icon
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "30 unchanged") {
			if line != "  30 unchanged" {
				t.Errorf("expected footer line to be %q, got %q", "  30 unchanged", line)
			}
			break
		}
	}
}

func TestRenderNestedSections_FooterOnly(t *testing.T) {
	sections := []NestedSection{
		{
			Title: "Networks",
			Footer: []DiffLine{
				{Type: Info, Message: "2 unchanged"},
			},
		},
	}

	out := StripANSI(RenderNestedSections(sections))

	if !strings.Contains(out, "Networks") {
		t.Errorf("expected output to contain section header 'Networks', got:\n%s", out)
	}
	if !strings.Contains(out, "2 unchanged") {
		t.Errorf("expected output to contain footer message '2 unchanged', got:\n%s", out)
	}
}
