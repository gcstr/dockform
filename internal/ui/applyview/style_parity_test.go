package applyview

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/gcstr/dockform/internal/planner"
	"github.com/gcstr/dockform/internal/ui"
)

// forceColor makes lipgloss emit real ANSI regardless of TTY, so these tests
// can see styling that the golden files (rendered without colour) cannot.
func forceColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	_ = os.Stderr
}

func seededModel(t *testing.T) Model {
	t.Helper()
	m := New(fixedClock(time.Second))
	return apply(m,
		tea.WindowSizeMsg{Width: 100},
		SeedMsg{Items: []planner.ResourceRef{
			{Context: "hetzner-one", Type: planner.ResourceVolume, Name: "data"},
			{Context: "hetzner-one", Type: planner.ResourceStack, Name: "linkwarden"},
			{Context: "hetzner-one", Type: planner.ResourceService, Name: "postgres", Parent: "linkwarden"},
		}},
	)
}

// The apply view must style its context header exactly as the plan renderer
// does, so the two read as one product. plan uses ui.SectionTitle (bold blue)
// for the context level.
func TestApplyView_ContextHeaderUsesPlanStyle(t *testing.T) {
	forceColor(t)
	m := seededModel(t)

	var found string
	for _, l := range m.bodyLines() {
		if strings.Contains(ansi.Strip(l.text), "hetzner-one") && !strings.Contains(ansi.Strip(l.text), "Volumes") {
			found = l.text
			break
		}
	}
	if found == "" {
		t.Fatal("no context header line rendered")
	}
	want := ui.SectionTitle("hetzner-one")
	if !strings.Contains(found, want) {
		t.Errorf("context header not styled like plan's\n got: %q\nwant to contain: %q", found, want)
	}
}

// Group titles sit below the context header, where plan uses its nested
// section style (bold italic).
func TestApplyView_GroupTitleUsesPlanNestedStyle(t *testing.T) {
	forceColor(t)
	m := seededModel(t)

	var found string
	for _, l := range m.bodyLines() {
		if strings.Contains(ansi.Strip(l.text), "Volumes") {
			found = l.text
			break
		}
	}
	if found == "" {
		t.Fatal("no Volumes group line rendered")
	}
	want := ui.NestedSectionTitle("Volumes")
	if !strings.Contains(found, want) {
		t.Errorf("group title not styled like plan's nested sections\n got: %q\nwant to contain: %q", found, want)
	}
}

// Styling must not change layout. ANSI escapes are zero-width on screen but
// not zero-length in a string, so any %-Ns padding applied around a styled
// title silently misaligns the column after it. Rendering with colour on and
// then stripping the escapes must reproduce the uncoloured render byte for
// byte.
func TestApplyView_StylingDoesNotChangeLayout(t *testing.T) {
	plainLines := func() []string {
		m := seededModel(t)
		var out []string
		for _, l := range m.bodyLines() {
			out = append(out, l.text)
		}
		return out
	}

	// Colour off (the default in tests).
	want := plainLines()

	forceColor(t)
	coloured := plainLines()

	if len(coloured) != len(want) {
		t.Fatalf("line count changed with colour: got %d, want %d", len(coloured), len(want))
	}
	for i := range want {
		if got := ansi.Strip(coloured[i]); got != want[i] {
			t.Errorf("line %d layout changed once styled\n got: %q\nwant: %q", i, got, want[i])
		}
	}
}

// bodyLines feeds View through fitBody, which truncates and windows. Assert at
// the View boundary too, so styling that survives bodyLines but is lost on the
// way out would still be caught.
func TestApplyView_ViewCarriesPlanStyles(t *testing.T) {
	forceColor(t)
	got := seededModel(t).View()

	for _, want := range []string{
		ui.SectionTitle("hetzner-one"),
		ui.NestedSectionTitle("Volumes"),
		ui.NestedSectionTitle("Stacks"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("View() missing plan styling %q", want)
		}
	}
}
