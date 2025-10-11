package dashboardcmd

import "testing"

func TestCommandPaletteWidthBounds(t *testing.T) {
	if got := commandPaletteWidth(10); got != 8 {
		t.Fatalf("expected width to clamp to available terminal width, got %d", got)
	}
	if got := commandPaletteWidth(200); got != commandPaletteMaxWidth {
		t.Fatalf("expected width to cap at max, got %d", got)
	}
}

func TestCommandListContentWidthPositive(t *testing.T) {
	if got := commandListContentWidth(commandPaletteMinWidth); got <= 0 {
		t.Fatalf("expected positive content width, got %d", got)
	}
	if commandListContentWidth(1) != 1 {
		t.Fatalf("expected minimum width to be 1 when palette is tiny")
	}
}
