package tui

import (
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

// popupLines splits a rendered popup into its physical (newline-separated) lines. ANSI escapes
// never carry a newline, so splitting the raw string is safe and keeps the styling for the width
// and SGR assertions.
func popupLines(out string) []string {
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// Every rendered line — border runes included — is exactly the total width it was handed, with
// and without the optional title / hint rows, so the pane's right border always lands on the
// same column (the lipgloss v2 total-width contract renderStartupBox relies on).
func TestRenderPopupLinesAreExactWidth(t *testing.T) {
	th := newTheme()
	base := popupSpec{
		title:    "saved sessions  (this workspace)",
		rows:     []string{"first row", "second row", "third row"},
		selected: 1,
		hint:     "esc close",
		maxRows:  8,
	}
	cases := map[string]popupSpec{
		"title+hint": base,
		"no title":   {rows: base.rows, selected: base.selected, hint: base.hint, maxRows: base.maxRows},
		"no hint":    {title: base.title, rows: base.rows, selected: base.selected, maxRows: base.maxRows},
		"rows only":  {rows: base.rows, selected: base.selected, maxRows: base.maxRows},
	}
	for name, spec := range cases {
		spec := spec
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, width := range []int{40, 60, 98} {
				out := renderPopup(th, spec, width)
				for i, ln := range popupLines(out) {
					if w := lipgloss.Width(ln); w != width {
						t.Errorf("width %d: line %d is %d cells, want %d: %q", width, i, w, width, strip(ln))
					}
				}
			}
		})
	}
}

// A row wider than the inner budget is truncated, never wrapped: the pane's physical line count
// is exactly 2 (borders) + title + shown rows + hint, and the long row ends in an ellipsis.
func TestRenderPopupLongRowDoesNotWrap(t *testing.T) {
	th := newTheme()
	spec := popupSpec{
		title:    "commands",
		rows:     []string{"short", strings.Repeat("verylongtoken ", 12), "also short"},
		selected: 1,
		hint:     "esc dismiss",
		maxRows:  8,
	}
	out := renderPopup(th, spec, 40)
	lines := popupLines(out)

	wantLines := 2 + 1 + len(spec.rows) + 1 // borders + title + rows + hint
	if len(lines) != wantLines {
		t.Fatalf("popup has %d physical lines, want %d (a long row must truncate, not wrap):\n%s",
			len(lines), wantLines, strip(out))
	}
	if !strings.Contains(strip(out), "…") {
		t.Errorf("long row was not truncated to an ellipsis:\n%s", strip(out))
	}
}

// The selected row (within the scrolled window) carries the ❯ marker and the userBlock highlight
// SGR; the check is a loose contains on the un-stripped output, not a byte golden, so a lipgloss
// renderer change cannot false-fail it.
func TestRenderPopupSelectedRowHighlight(t *testing.T) {
	th := newTheme()
	spec := popupSpec{
		title:    "files",
		rows:     []string{"one.go", "two.go", "three.go"},
		selected: 2,
		hint:     "esc dismiss",
		maxRows:  8,
	}
	out := renderPopup(th, spec, 50)

	if !strings.Contains(strip(out), glyphUser+" three.go") {
		t.Errorf("selected row missing the %q marker:\n%s", glyphUser, strip(out))
	}
	probe := th.userBlock.Render("x")
	sgr := probe[:strings.IndexByte(probe, 'm')+1] // the userBlock fg+bg SGR, up to and including its 'm'
	if !strings.Contains(out, sgr) {
		t.Errorf("selected row carries no userBlock highlight SGR %q", sgr)
	}
}

// A spec with selected = −1 paints no marker and no highlight: every row is faint, no ❯ appears,
// and the userBlock SGR is absent from the output.
func TestRenderPopupNoSelection(t *testing.T) {
	th := newTheme()
	spec := popupSpec{
		title:    "skills",
		rows:     []string{"alpha", "beta", "gamma"},
		selected: -1,
		hint:     "esc dismiss",
		maxRows:  8,
	}
	out := renderPopup(th, spec, 50)

	if strings.Contains(strip(out), glyphUser) {
		t.Errorf("selected = -1 still painted a %q marker:\n%s", glyphUser, strip(out))
	}
	probe := th.userBlock.Render("x")
	sgr := probe[:strings.IndexByte(probe, 'm')+1]
	if strings.Contains(out, sgr) {
		t.Errorf("selected = -1 still carries the userBlock highlight SGR %q", sgr)
	}
}

// An empty title drops the title row and an empty hint drops the hint row — each is one fewer
// physical line than the same spec with the field set, and the dropped text is absent.
func TestRenderPopupEmptyTitleAndHintDropRows(t *testing.T) {
	th := newTheme()
	full := popupSpec{
		title:    "saved sessions",
		rows:     []string{"row one", "row two"},
		selected: 0,
		hint:     "esc close",
		maxRows:  8,
	}
	const width = 50
	fullLines := len(popupLines(renderPopup(th, full, width)))

	noTitle := full
	noTitle.title = ""
	noTitleOut := renderPopup(th, noTitle, width)
	if got := len(popupLines(noTitleOut)); got != fullLines-1 {
		t.Errorf("empty title kept its row: %d lines, want %d", got, fullLines-1)
	}
	if strings.Contains(strip(noTitleOut), "saved sessions") {
		t.Errorf("empty title still rendered its text:\n%s", strip(noTitleOut))
	}

	noHint := full
	noHint.hint = ""
	noHintOut := renderPopup(th, noHint, width)
	if got := len(popupLines(noHintOut)); got != fullLines-1 {
		t.Errorf("empty hint kept its row: %d lines, want %d", got, fullLines-1)
	}
	if strings.Contains(strip(noHintOut), "esc close") {
		t.Errorf("empty hint still rendered its text:\n%s", strip(noHintOut))
	}
}

// popupRowWindow shows every row when the list fits the cap, keeps the window capped and roughly
// centred on a mid-list selection, and clamps at both ends — the cases the old inline windowing
// satisfied.
func TestPopupRowWindow(t *testing.T) {
	cases := []struct {
		name                    string
		selected, total, capRow int
		wantStart, wantEnd      int
	}{
		{"fits under cap", 3, 5, 8, 0, 5},
		{"exactly at cap", 4, 8, 8, 0, 8},
		{"mid centred", 10, 30, 8, 6, 14},
		{"clamp at start", 1, 30, 8, 0, 8},
		{"clamp at end", 29, 30, 8, 22, 30},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			start, end := popupRowWindow(c.selected, c.total, c.capRow)
			if start != c.wantStart || end != c.wantEnd {
				t.Errorf("popupRowWindow(%d,%d,%d) = [%d,%d), want [%d,%d)",
					c.selected, c.total, c.capRow, start, end, c.wantStart, c.wantEnd)
			}
			if end-start > c.capRow {
				t.Errorf("window [%d,%d) exceeds cap %d", start, end, c.capRow)
			}
		})
	}
}

// A degenerate width (≤ the border frame size, where a box cannot fit even one content cell)
// neither panics nor overflows: renderPopup degrades to an empty pane rather than a box wider
// than the window it was handed.
func TestRenderPopupDegenerateWidth(t *testing.T) {
	th := newTheme()
	frame := th.popupBorder.GetHorizontalFrameSize()
	spec := popupSpec{
		title:    "saved sessions",
		rows:     []string{"a row that is far wider than the tiny width"},
		selected: 0,
		hint:     "esc close",
		maxRows:  8,
	}
	for _, width := range []int{frame, frame - 1, 1, 0} {
		out := renderPopup(th, spec, width) // must not panic
		for i, ln := range popupLines(out) {
			if w := lipgloss.Width(ln); w > width {
				t.Errorf("width %d: line %d is %d cells, exceeds the window width: %q", width, i, w, strip(ln))
			}
		}
	}
}
