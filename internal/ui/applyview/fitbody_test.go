package applyview

import (
	"fmt"
	"testing"
)

// fitBody is the one piece of this view that can silently break the very
// guarantee it exists for: hand Bubble Tea more lines than the terminal has and
// it keeps only the last `height`, discarding the header. A whole-branch review
// found the original implementation emitting budget+2 lines at budgets of 1 and 2,
// because the settle loop clamped its window to at least one line without checking
// the budget could also afford both markers. Only realistic heights (24/40/50) were
// tested, so nothing caught it.
//
// This sweeps the small budgets exhaustively rather than sampling them.
func TestFitBodyNeverExceedsBudget(t *testing.T) {
	for n := 0; n <= 15; n++ {
		lines := make([]bodyLine, n)
		for i := range lines {
			lines[i] = bodyLine{text: fmt.Sprintf("line%d", i)}
		}
		for budget := 0; budget <= 8; budget++ {
			// every contiguous active span, plus the no-active case
			for first := -1; first < n; first++ {
				for last := first; last < n; last++ {
					for i := range lines {
						lines[i].active = first >= 0 && i >= first && i <= last
					}
					got := fitBody(lines, budget)
					if len(got) > budget {
						t.Fatalf("n=%d budget=%d active=[%d,%d]: got %d lines, over budget:\n%v",
							n, budget, first, last, len(got), got)
					}
					if budget > 0 && n > 0 && len(got) == 0 {
						t.Fatalf("n=%d budget=%d active=[%d,%d]: returned nothing with room to render",
							n, budget, first, last)
					}
				}
			}
		}
	}
}

// When there is active work and room to show it, it must be in the window —
// showing the top of a hundred-line list while the action is at the bottom is the
// failure this windowing exists to avoid.
func TestFitBodyKeepsActiveVisible(t *testing.T) {
	n := 40
	for _, active := range []int{0, 7, 20, 39} {
		lines := make([]bodyLine, n)
		for i := range lines {
			lines[i] = bodyLine{text: fmt.Sprintf("line%d", i), active: i == active}
		}
		for budget := 3; budget <= 12; budget++ {
			got := fitBody(lines, budget)
			want := fmt.Sprintf("line%d", active)
			found := false
			for _, g := range got {
				if g == want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("budget=%d active=%d: %q missing from window:\n%v", budget, active, want, got)
			}
		}
	}
}
