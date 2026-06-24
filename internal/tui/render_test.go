package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Sticky-to-top offset drift guard (P2.7)
// ----------------------------------------------------------------------------

// TestWrappedOffsetMatchesViewport pins wrappedOffset to the viewport's own soft-wrap
// accounting: summing every physical line's wrapped height must equal the viewport's
// TotalLineCount for the same lines at the same width. If the viewport's calculateLine ever
// changes (or a border/gutter is added, breaking the maxWidth == Width assumption), this
// fails — and the sticky-to-top SetYOffset would silently mis-pin without it.
func TestWrappedOffsetMatchesViewport(t *testing.T) {
	const width = 10
	lines := []string{
		"short",                 // 1 row
		strings.Repeat("a", 25), // 3 rows (ceil 25/10)
		"",                      // 1 row (a blank line still occupies a row)
		"thirteen char",         // 2 rows (13 cols)
		strings.Repeat("b", 10), // 1 row (exactly the width)
		strings.Repeat("c", 11), // 2 rows
	}

	vp := viewport.New()
	vp.SoftWrap = true
	vp.SetWidth(width)
	vp.SetHeight(5)
	vp.SetContentLines(lines)

	if got, want := wrappedOffset(lines, width), vp.TotalLineCount(); got != want {
		t.Errorf("wrappedOffset = %d; want the viewport's TotalLineCount %d (soft-wrap drift)", got, want)
	}
}

// A blank line and a zero-width input are each a single row, not zero, matching the
// viewport's max(1, …) floor.
func TestWrappedOffsetFloors(t *testing.T) {
	if got := wrappedOffset([]string{"", "", ""}, 10); got != 3 {
		t.Errorf("three blank lines = %d rows; want 3 (each floors to one)", got)
	}
	if got := wrappedOffset([]string{""}, 0); got != 1 {
		t.Errorf("zero width = %d rows; want 1 (width floored to one, no divide-by-zero)", got)
	}
}

// ----------------------------------------------------------------------------
// Sub-agent framing reflow safety (P3.14)
// ----------------------------------------------------------------------------

// railedWidth floors a deeply-nested block's usable width at one column so the wrapper never
// divides by zero, even when the rail gutters consume more than the whole terminal width.
func TestRailedWidthFloors(t *testing.T) {
	if got := railedWidth(80, 0); got != 80 {
		t.Errorf("railedWidth(80, 0) = %d; want 80 (depth 0 takes no gutter)", got)
	}
	if got := railedWidth(3, 5); got != 1 {
		t.Errorf("railedWidth(3, 5) = %d; want 1 (floored, not negative)", got)
	}
}

// A Depth > 0 block renders at a tiny and a zero width without panicking (the acceptance's
// "reflow at small sizes doesn't panic"); the framed text is still produced.
func TestSubAgentReflowAtSmallWidths(t *testing.T) {
	for _, width := range []int{0, 1, 2, 3, 6} {
		tr := feed(domain.MessageEvent{
			EventBase: domain.EventBase{Depth: 2},
			Text:      "a deeply nested sub-agent message that must wrap hard",
		})
		lines := tr.renderLines(newTheme(), width) // must not panic at any width
		if len(lines) == 0 {
			t.Errorf("width %d produced no lines", width)
		}
	}
}
