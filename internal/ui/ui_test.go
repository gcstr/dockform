package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestStripANSI_RemovesCodes(t *testing.T) {
	in := "\x1b[31mred\x1b[0m and normal"
	got := StripANSI(in)
	if got != "red and normal" {
		t.Fatalf("expected ANSI to be stripped, got: %q", got)
	}
}

func TestStdPrinter_WritesToCorrectStreams_WithPrefixes(t *testing.T) {
	var out bytes.Buffer
	var err bytes.Buffer
	p := StdPrinter{Out: &out, Err: &err}

	p.Plain("hello %s", "plain")
	p.Info("hello %s", "world")
	p.Warn("warn %d", 1)
	p.Error("error %s", "boom")

	outStr := StripANSI(out.String())
	errStr := StripANSI(err.String())

	if !strings.Contains(outStr, "hello plain\n") {
		t.Fatalf("expected plain text on stdout, got: %q", outStr)
	}
	if !strings.Contains(outStr, "[info] hello world\n") {
		t.Fatalf("expected info prefix on stdout, got: %q", outStr)
	}
	if !strings.Contains(errStr, "[warn] warn 1\n") {
		t.Fatalf("expected warn prefix on stderr, got: %q", errStr)
	}
	if !strings.Contains(errStr, "[error] error boom\n") {
		t.Fatalf("expected error prefix on stderr, got: %q", errStr)
	}
}

func TestRenderSectionedList_ShowsItemsWithIcons(t *testing.T) {
	sections := []Section{
		{
			Title: "Applications",
			Items: []DiffLine{
				Line(Noop, "noop item"),
				Line(Add, "add item"),
				Line(Remove, "remove item"),
				Line(Change, "change item"),
			},
		},
		{ // empty section is skipped
			Title: "Empty",
			Items: nil,
		},
	}
	got := StripANSI(RenderSectionedList(sections))

	// Check that the output has the expected structure
	expected := []string{
		"Applications",  // Section header
		"  ✓ noop item", // Two-space indented items with icons
		"  ↑ add item",
		"  × remove item",
		"  → change item",
	}

	for _, exp := range expected {
		if !strings.Contains(got, exp) {
			t.Fatalf("expected sectioned list to contain %q, got: %q", exp, got)
		}
	}

	// Should not contain "Empty" section since it has no items
	if strings.Contains(got, "Empty") {
		t.Fatalf("expected empty sections to be skipped, got: %q", got)
	}
}

func TestSpinner_NoTTY_NoOutput(t *testing.T) {
	var out bytes.Buffer
	s := NewSpinner(&out, "Loading...")
	s.Start()
	s.Stop()
	if out.Len() != 0 {
		t.Fatalf("expected spinner to produce no output when not a TTY, got: %q", out.String())
	}
}

func TestProgress_NoTTY_NoOutput(t *testing.T) {
	var out bytes.Buffer
	p := NewProgress(&out, "Applying")
	p.Start(3)
	p.Increment()
	p.SetAction("doing work")
	p.Stop()
	if out.Len() != 0 {
		t.Fatalf("expected progress to produce no output when not a TTY, got: %q", out.String())
	}
}

func TestRenderSectionedList_EmptyReturnsEmpty(t *testing.T) {
	got := RenderSectionedList([]Section{{Title: "A", Items: nil}, {Title: "B", Items: []DiffLine{}}})
	if got != "" {
		t.Fatalf("expected empty string for empty sections, got: %q", got)
	}
}

func TestRenderNestedSections_ShowsNestedStructure(t *testing.T) {
	sections := []NestedSection{
		{
			Title: "Filesets",
			Sections: []NestedSection{
				{
					Title: "website",
					Items: []DiffLine{
						Line(Add, "create config.yaml"),
						Line(Change, "update index.html"),
					},
				},
				{
					Title: "assets",
					Items: []DiffLine{
						Line(Remove, "delete old.css"),
					},
				},
			},
		},
		{
			Title: "Applications",
			Items: []DiffLine{
				Line(Noop, "app1 running"),
			},
		},
	}
	got := StripANSI(RenderNestedSections(sections))

	// Check nested structure with proper indentation
	expected := []string{
		"Filesets",                 // Main section header
		"  website",                // Nested section header (2 spaces)
		"    ↑ create config.yaml", // Nested items (4 spaces)
		"    → update index.html",
		"  assets",             // Another nested section
		"    × delete old.css", // Its items
		"Applications",         // Regular section
		"  ✓ app1 running",     // Regular items (2 spaces)
	}

	for _, exp := range expected {
		if !strings.Contains(got, exp) {
			t.Fatalf("expected nested sections to contain %q, got: %q", exp, got)
		}
	}
}

func TestStdPrinter_NilWriters_NoPanic(t *testing.T) {
	p := StdPrinter{}
	// Should be no-ops when writers are nil
	p.Plain("hello")
	p.Info("world")
	p.Warn("warn")
	p.Error("err")
}

func TestConfirmModel_ViewContainsPrompts(t *testing.T) {
	m := newConfirmModel()
	v := StripANSI(m.View())
	if v == "" || !containsAll(v, []string{"Dockform will apply", "Type", "Answer:"}) {
		t.Fatalf("expected view to contain prompt text, got: %q", v)
	}
}

func TestItalicStripsToPlain(t *testing.T) {
	rendered := Italic("manifest.yml")
	if !strings.Contains(rendered, "manifest.yml") {
		t.Fatalf("expected italic output to contain original text, got: %q", rendered)
	}
	if StripANSI(rendered) != "manifest.yml" {
		t.Fatalf("expected italic output to strip to plain text, got: %q", StripANSI(rendered))
	}
}

func TestSuccessAndColorHelpers(t *testing.T) {
	if StripANSI(SuccessMark()) != "✓" {
		t.Fatalf("expected success mark to render check")
	}
	if StripANSI(RedText("boom")) != "boom" {
		t.Fatalf("expected red text to strip to original value")
	}
	if StripANSI(ConfirmToken("yes")) != "yes" {
		t.Fatalf("expected confirm token to strip to original")
	}
	for _, fn := range []func(string) string{GreenText, YellowText, BlueText} {
		if StripANSI(fn("text")) != "text" {
			t.Fatalf("expected color helper to strip to original text")
		}
	}
}

func TestSectionTitleAndPlanSummary(t *testing.T) {
	if StripANSI(SectionTitle("Apps")) == "" {
		t.Fatalf("expected section title output")
	}
	if !strings.Contains(FormatPlanSummary(1, 2, 3), "1 to create") {
		t.Fatalf("unexpected plan summary string")
	}
	if StripRedundantPrefixes("volume data", "Volumes") != "data" {
		t.Fatalf("expected volume prefix stripped")
	}
	if StripRedundantPrefixes("network net1", "Networks") != "net1" {
		t.Fatalf("expected network prefix stripped")
	}
}

func TestNoopPrinterAndLineHelpers(t *testing.T) {
	var np NoopPrinter
	np.Plain("noop")
	np.Info("noop")
	np.Warn("noop")
	np.Error("noop")
	if Line(Info, "value").String() != "value" {
		t.Fatalf("expected line string helper to return message")
	}
}

func TestClearCurrentLineIfTTY(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty open: %v", err)
	}
	t.Cleanup(func() {
		if err := master.Close(); err != nil {
			t.Fatalf("close master pty: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := slave.Close(); err != nil {
			t.Fatalf("close slave pty: %v", err)
		}
	})
	dataCh := make(chan string, 1)
	go func() {
		buf := make([]byte, 32)
		n, _ := master.Read(buf)
		dataCh <- string(buf[:n])
	}()
	clearCurrentLineIfTTY(slave)
	select {
	case data := <-dataCh:
		if !strings.HasPrefix(data, "\r\x1b[2K") {
			t.Fatalf("expected clear sequence, got %q", data)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out waiting for clear sequence")
	}
}

func TestStdPrinterSuppressedByEnv(t *testing.T) {
	var out bytes.Buffer
	p := StdPrinter{Out: &out, Err: &out}
	t.Setenv("DOCKFORM_TUI_ACTIVE", "1")
	p.Info("should not print")
	p.Warn("warn")
	p.Error("err")
	if out.Len() != 0 {
		t.Fatalf("expected suppressed output when TUI active")
	}
	t.Setenv("DOCKFORM_TUI_ACTIVE", "")
	p.Info("visible")
	p.Warn("warn")
	p.Error("err")
	if !strings.Contains(StripANSI(out.String()), "visible") {
		t.Fatalf("expected info output when env cleared")
	}
}

func TestSpinner_StartStop_Idempotent_NoTTY(t *testing.T) {
	var out bytes.Buffer
	s := NewSpinner(&out, "Working")
	s.Start()
	s.Start()
	s.Stop()
	s.Stop()
	if out.Len() != 0 {
		t.Fatalf("expected no output for non-tty spinner, got: %q", out.String())
	}
}

func TestProgress_Methods_NoTTY_NoPanic(t *testing.T) {
	var out bytes.Buffer
	p := NewProgress(&out, "Doing")
	p.Start(2)
	p.SetAction("step")
	p.Increment()
	p.AdjustTotal(-1)
	p.Stop()
}

// containsAll reports whether s contains all substrings in subs.
func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !bytes.Contains([]byte(s), []byte(sub)) {
			return false
		}
	}
	return true
}
