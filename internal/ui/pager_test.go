package ui

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestRenderYAMLInPagerTTY_NonTTY_FallsBackToPlainOutput(t *testing.T) {
	yamlContent := "docker:\n  context: test\n  identifier: myapp\n"
	title := "test.yml"

	var out bytes.Buffer
	var in bytes.Buffer

	// Non-TTY should fall back to plain output
	err := RenderYAMLInPagerTTY(&in, &out, yamlContent, title)
	if err != nil {
		t.Fatalf("RenderYAMLInPagerTTY: %v", err)
	}

	result := out.String()

	// Should contain the original YAML content
	if !strings.Contains(result, "docker:") {
		t.Fatalf("expected YAML content in output, got: %q", result)
	}

	// Should end with newline
	if !strings.HasSuffix(result, "\n") {
		t.Fatalf("expected trailing newline, got: %q", result)
	}

	// Should not contain ANSI codes (since it's plain output)
	if strings.Contains(result, "\x1b[") {
		t.Fatalf("expected no ANSI codes in plain output, got: %q", result)
	}
}

func TestRenderYAMLInPagerTTY_NonTTY_EnsuresTrailingNewline(t *testing.T) {
	yamlContent := "docker:\n  identifier: test" // No trailing newline
	title := "test.yml"

	var out bytes.Buffer
	var in bytes.Buffer

	err := RenderYAMLInPagerTTY(&in, &out, yamlContent, title)
	if err != nil {
		t.Fatalf("RenderYAMLInPagerTTY: %v", err)
	}

	result := out.String()

	// Should add trailing newline
	if !strings.HasSuffix(result, "\n") {
		t.Fatalf("expected trailing newline to be added, got: %q", result)
	}
}

func TestColorizeYAML_AppliesHighlighting(t *testing.T) {
	yamlContent := "docker:\n  context: test\n"

	highlighted := colorizeYAML(yamlContent)

	// Should contain ANSI escape codes (indicating highlighting was applied)
	if !strings.Contains(highlighted, "\x1b[") {
		t.Fatalf("expected ANSI codes in highlighted output, got: %q", highlighted)
	}

	// Should still contain the original content (though with ANSI codes)
	// We use StripANSI to check the underlying content
	stripped := StripANSI(highlighted)
	if !strings.Contains(stripped, "docker:") {
		t.Fatalf("expected original content preserved after stripping ANSI, got: %q", stripped)
	}
}

func TestColorizeYAML_FallsBackOnError(t *testing.T) {
	// Chroma is actually quite robust and will highlight even invalid YAML
	// So let's test that it at least doesn't crash and returns some output
	invalidYAML := "this is not valid yaml: [[[["

	result := colorizeYAML(invalidYAML)

	// Should not be empty and should contain some form of the original content
	if len(result) == 0 {
		t.Fatalf("expected non-empty result, got empty string")
	}

	// Should contain some recognizable content (may be highlighted)
	stripped := StripANSI(result)
	if !strings.Contains(stripped, "this is not valid yaml") {
		t.Fatalf("expected some original content preserved, got: %q", stripped)
	}
}

func TestFormatContentWithLineNumbers_AddsLineNumbers(t *testing.T) {
	content := "line1\nline2\nline3"

	formatted := formatContentWithLineNumbers(content)

	// Should contain line numbers
	if !strings.Contains(formatted, "   1 │ ") {
		t.Fatalf("expected line number 1, got: %q", formatted)
	}

	if !strings.Contains(formatted, "   2 │ ") {
		t.Fatalf("expected line number 2, got: %q", formatted)
	}

	if !strings.Contains(formatted, "   3 │ ") {
		t.Fatalf("expected line number 3, got: %q", formatted)
	}

	// Should preserve original content
	if !strings.Contains(formatted, "line1") {
		t.Fatalf("expected original content preserved, got: %q", formatted)
	}
}

func TestFormatContentWithLineNumbers_MinimumWidth(t *testing.T) {
	// Test with single line to ensure minimum 4-digit width
	content := "single line"

	formatted := formatContentWithLineNumbers(content)

	// Should use minimum 4-character width for line numbers
	if !strings.Contains(formatted, "   1 │ ") {
		t.Fatalf("expected 4-character width line number, got: %q", formatted)
	}
}

func TestFormatContentWithLineNumbers_LargeLineNumbers(t *testing.T) {
	// Create content with many lines to test dynamic width
	lines := make([]string, 1500)
	for i := range lines {
		lines[i] = "content"
	}
	content := strings.Join(lines, "\n")

	formatted := formatContentWithLineNumbers(content)

	// Should handle large line numbers (1500 should be 4 digits)
	if !strings.Contains(formatted, "1500 │ ") {
		t.Fatalf("expected line number 1500, got substring: %q",
			formatted[len(formatted)-50:]) // Show end of formatted content
	}
}

func TestPagerModel_HeaderView_CreatesCorrectFormat(t *testing.T) {
	content := "line1\nline2\nline3"
	title := "test.yml"

	m := newPagerModel(title, content)
	m.viewport.Width = 80 // Set a reasonable width

	header := m.headerView()

	// Should contain the title
	if !strings.Contains(header, "test.yml") {
		t.Fatalf("expected title in header, got: %q", header)
	}

	// Should contain separator characters
	if !strings.Contains(header, "─") {
		t.Fatalf("expected separator chars in header, got: %q", header)
	}

	// Should contain junction characters
	if !strings.Contains(header, "┬") {
		t.Fatalf("expected top junction in header, got: %q", header)
	}

	if !strings.Contains(header, "┼") {
		t.Fatalf("expected middle junction in header, got: %q", header)
	}
}

func TestPagerModel_FooterView_CreatesBottomSeparator(t *testing.T) {
	content := "line1\nline2\nline3"
	title := "test.yml"

	m := newPagerModel(title, content)
	m.viewport.Width = 80

	footer := m.footerView()

	// Should contain separator characters
	if !strings.Contains(footer, "─") {
		t.Fatalf("expected separator chars in footer, got: %q", footer)
	}
}

func TestPagerModel_SmallWidth_HandlesGracefully(t *testing.T) {
	content := "test"
	title := "test.yml"

	m := newPagerModel(title, content)
	m.viewport.Width = 5 // Very small width

	header := m.headerView()
	footer := m.footerView()

	// Should handle small widths gracefully (return empty or minimal content)
	// The exact behavior isn't as important as not crashing
	if len(header) > 100 || len(footer) > 100 {
		t.Fatalf("expected reasonable output for small width, got header: %q, footer: %q", header, footer)
	}
}


func TestRenderYAMLInPagerTTY_WriteError_ReturnsError(t *testing.T) {
	yamlContent := "docker:\n  identifier: test\n"
	title := "test.yml"

	// Create a writer that always fails
	failWriter := &failingWriter{}
	var in bytes.Buffer

	err := RenderYAMLInPagerTTY(&in, failWriter, yamlContent, title)
	if err == nil {
		t.Fatalf("expected error from failing writer, got nil")
	}
}

// failingWriter always returns an error on Write
type failingWriter struct{}

func (fw *failingWriter) Write(p []byte) (n int, err error) {
	return 0, io.ErrClosedPipe
}
