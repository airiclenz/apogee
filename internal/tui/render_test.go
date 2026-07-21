package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/charmbracelet/x/ansi"
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

// ----------------------------------------------------------------------------
// The tool header's label styling
// ----------------------------------------------------------------------------

// A tool header shows "Label target" with no brackets, and the label alone carries the
// bold-orange style — baked in before the wrap, so the visible text is unaffected. The style
// assertion is a loose contains against the theme's own render rather than a byte-exact
// golden, so a lipgloss change cannot false-fail it; the guard above it catches the opposite
// failure, a toolLabel role that paints nothing at all.
func TestToolHeaderLabelStyled(t *testing.T) {
	th := newTheme()
	head := renderToolBlock(th, toolView{Label: "Read File", Target: "main.go"}, 80)[0]

	if got, want := ansi.Strip(head), "✦ Read File main.go"; got != want {
		t.Errorf("header text = %q; want %q (no brackets)", got, want)
	}
	styled := th.toolLabel.Render("Read File")
	if styled == "Read File" {
		t.Fatal("the toolLabel role renders no escape sequence; the header would be unstyled")
	}
	if !strings.Contains(head, styled) {
		t.Errorf("header %q does not carry the styled label %q", head, styled)
	}
}

// ----------------------------------------------------------------------------
// inputContentRows sizes the prompt box to what the textarea actually draws
// ----------------------------------------------------------------------------

// TestInputContentRows pins the box-sizing count against the textarea's own wrap, including the
// edge that used to under-count: a logical line whose final wrapped segment exactly fills the
// width takes one extra visual row (the widget reserves a trailing row for the caret past a full
// line). Under-counting it left the box a row short at the wrap boundary, stranding the scroll the
// layout re-seat then could not clamp (ISSUES #2).
func TestInputContentRows(t *testing.T) {
	const w = 10
	cases := []struct {
		name  string
		value string
		want  int
	}{
		{"empty is one row", "", 1},
		{"short line", "abc", 1},
		{"one under the width", strings.Repeat("a", 9), 1},
		{"exact width gains a trailing row", strings.Repeat("a", 10), 2},
		{"one over the width", strings.Repeat("a", 11), 2},
		{"two full widths", strings.Repeat("a", 20), 3},
		{"two full widths plus one", strings.Repeat("a", 21), 3},
		{"trailing newline adds a row", "abc\n", 2},
		{"two logical lines", "abc\ndef", 2},
		{"each full logical line gets its trailing row", strings.Repeat("a", 10) + "\n" + strings.Repeat("b", 10), 4},
		{"wide glyphs count by display cells", strings.Repeat("あ", 5), 2}, // 5×2 = 10 cells = exact width
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inputContentRows(c.value, w); got != c.want {
				t.Errorf("inputContentRows(%q, %d) = %d, want %d", c.value, w, got, c.want)
			}
		})
	}
}

// A zero or negative width floors to one column rather than dividing by zero, and still returns at
// least one row.
func TestInputContentRowsZeroWidth(t *testing.T) {
	if got := inputContentRows("ab", 0); got < 1 {
		t.Errorf("inputContentRows with zero width = %d, want >= 1 (width floored to one)", got)
	}
}
