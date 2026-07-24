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

// popupInterior strips a rendered popup line of its ANSI styling and its border+padding chrome,
// leaving just the content text — so an exact-match assertion sees "a" rather than "│ a       │".
func popupInterior(line string) string {
	return strings.Trim(strip(line), "│ ")
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

// The pane is filled solid black end to end: every printable cell — border runes, the black
// padding columns, title/row/hint text, and crucially the gap after a row shorter than the box —
// carries an explicit background (black, or the selected row's dark-gray highlight bar), so no
// cell is ever left on the terminal's default background. This is the regression guard for the
// "black-hole strip after short rows" bug: a bg-free line would surface as a bare cell here.
func TestRenderPopupIsFullyBlackFilled(t *testing.T) {
	th := newTheme()
	spec := popupSpec{
		title:    "saved sessions",                                       // shorter than the box
		rows:     []string{"x", "a much wider row label goes here", "y"}, // mix of short + wide
		selected: 1,                                                      // the wide row is the dark-gray highlight bar
		hint:     "esc close",                                            // shorter than the box
		maxRows:  8,
	}
	for _, width := range []int{40, 60, 98} {
		for i, ln := range popupLines(renderPopup(th, spec, width)) {
			if col, ok := firstCellWithoutBackground(ln); !ok {
				t.Errorf("width %d: line %d has a bare (no-background) cell at column %d: %q",
					width, i, col, strip(ln))
			}
		}
	}
}

// firstCellWithoutBackground walks a rendered line's SGR stream and reports the column of the
// first printable cell whose background is unset (terminal default). ok is true when every cell
// has a background. It tracks only the background attribute: 48;5;n / 48;2;r;g;b / the 40-47 and
// 100-107 basics set it; 49 and a 0/empty reset (\e[m) clear it.
func firstCellWithoutBackground(line string) (col int, ok bool) {
	bgSet := false
	runes := []rune(line)
	for i := 0; i < len(runes); {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && !((runes[j] >= 'A' && runes[j] <= 'Z') || (runes[j] >= 'a' && runes[j] <= 'z')) {
				j++
			}
			if j < len(runes) && runes[j] == 'm' {
				bgSet = applySGRBackground(bgSet, string(runes[i+2:j]))
			}
			i = j + 1
			continue
		}
		if !bgSet {
			return col, false
		}
		col++
		i++
	}
	return 0, true
}

// applySGRBackground folds one SGR parameter list into the running background-set state.
func applySGRBackground(bgSet bool, params string) bool {
	if params == "" {
		return false // \e[m — full reset
	}
	fields := strings.Split(params, ";")
	for k := 0; k < len(fields); k++ {
		switch fields[k] {
		case "0":
			bgSet = false
		case "49":
			bgSet = false
		case "48": // extended background: 48;5;n or 48;2;r;g;b — consume its arguments
			bgSet = true
			if k+1 < len(fields) && fields[k+1] == "5" {
				k += 2
			} else if k+1 < len(fields) && fields[k+1] == "2" {
				k += 4
			}
		case "40", "41", "42", "43", "44", "45", "46", "47",
			"100", "101", "102", "103", "104", "105", "106", "107":
			bgSet = true
		}
	}
	return bgSet
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

// A body longer than the inner budget word-wraps to several rows — each still exactly the box
// width, none wider than the pane — rather than truncating like a row. The body is the module's
// one wrapping content block.
func TestRenderPopupBodyWraps(t *testing.T) {
	th := newTheme()
	const width = 40
	out := renderPopup(th, popupSpec{body: strings.Repeat("word ", 40)}, width)
	lines := popupLines(out)
	if len(lines) <= 2+1 { // 2 borders + more than one body row
		t.Fatalf("body did not wrap to multiple rows: %d physical lines:\n%s", len(lines), strip(out))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, width, strip(ln))
		}
	}
}

// Embedded newlines in the body are layout, not text to reflow: a body "a\n\nb" renders three body
// rows with a blank middle row — the approval reason/args separator case.
func TestRenderPopupBodyPreservesNewlines(t *testing.T) {
	th := newTheme()
	lines := popupLines(renderPopup(th, popupSpec{body: "a\n\nb"}, 40))
	if len(lines) != 2+3 { // 2 borders + 3 body rows
		t.Fatalf("body \"a\\n\\nb\" produced %d physical lines, want 5:\n%s", len(lines), strip(strings.Join(lines, "\n")))
	}
	if got := popupInterior(lines[1]); got != "a" {
		t.Errorf("first body row = %q, want \"a\"", got)
	}
	if got := popupInterior(lines[2]); got != "" {
		t.Errorf("middle body row = %q, want blank", got)
	}
	if got := popupInterior(lines[3]); got != "b" {
		t.Errorf("third body row = %q, want \"b\"", got)
	}
}

// A single token wider than the inner budget hard-breaks across body rows (wrapText's guarantee),
// so an unbroken blob can never blow past the pane's right edge.
func TestRenderPopupBodyHardBreaksLongToken(t *testing.T) {
	th := newTheme()
	const width = 20
	out := renderPopup(th, popupSpec{body: strings.Repeat("x", 100)}, width)
	lines := popupLines(out)
	if len(lines) <= 2+1 {
		t.Fatalf("over-long token did not hard-break: %d physical lines:\n%s", len(lines), strip(out))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, width, strip(ln))
		}
	}
}

// maxBodyRows caps the wrapped block: past the cap it keeps maxBodyRows−1 lines plus a faint
// "… (+N more lines)" marker (N = hidden lines) for exactly maxBodyRows body rows; a body at the cap
// shows every line with no marker; maxBodyRows ≤ 0 shows everything.
func TestRenderPopupBodyMaxRows(t *testing.T) {
	th := newTheme()
	const width = 40
	tenLines := strings.Join([]string{"l0", "l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9"}, "\n")

	capped := popupLines(renderPopup(th, popupSpec{body: tenLines, maxBodyRows: 4}, width))
	if got := len(capped) - 2; got != 4 { // less the two borders
		t.Fatalf("cap 4 rendered %d body rows, want 4:\n%s", got, strip(strings.Join(capped, "\n")))
	}
	if last := popupInterior(capped[len(capped)-2]); last != "… (+7 more lines)" {
		t.Errorf("last body row = %q, want \"… (+7 more lines)\"", last)
	}

	atCap := popupLines(renderPopup(th, popupSpec{body: "a\nb\nc\nd", maxBodyRows: 4}, width))
	if got := len(atCap) - 2; got != 4 {
		t.Fatalf("body exactly at cap rendered %d body rows, want 4:\n%s", got, strip(strings.Join(atCap, "\n")))
	}
	if joined := strip(strings.Join(atCap, "\n")); strings.Contains(joined, "more lines") {
		t.Errorf("body exactly at cap emitted an overflow marker:\n%s", joined)
	}

	uncapped := popupLines(renderPopup(th, popupSpec{body: tenLines, maxBodyRows: 0}, width))
	if got := len(uncapped) - 2; got != 10 {
		t.Errorf("maxBodyRows 0 rendered %d body rows, want all 10", got)
	}
}

// Composition order is title / body / rows / hint: the body sits below the title and above the
// rows, the rows still truncate (never wrap) and keep their selected-row highlight, and an empty
// body adds no rows.
func TestRenderPopupBodyComposition(t *testing.T) {
	th := newTheme()
	spec := popupSpec{
		title:    "the assistant is asking:",
		body:     "one line of body",
		rows:     []string{"yes", strings.Repeat("verylongword ", 12)},
		selected: 0,
		hint:     "esc cancel",
		maxRows:  8,
	}
	const width = 50
	lines := popupLines(renderPopup(th, spec, width))
	if len(lines) != 2+1+1+2+1 { // borders + title + body + 2 rows + hint
		t.Fatalf("composed popup has %d physical lines, want 7:\n%s", len(lines), strip(strings.Join(lines, "\n")))
	}
	if !strings.Contains(strip(lines[1]), "the assistant is asking:") {
		t.Errorf("title is not the first content row: %q", strip(lines[1]))
	}
	if !strings.Contains(strip(lines[2]), "one line of body") {
		t.Errorf("body does not sit directly below the title: %q", strip(lines[2]))
	}
	if !strings.Contains(strip(lines[3]), glyphUser+" yes") {
		t.Errorf("selected row does not follow the body with its marker: %q", strip(lines[3]))
	}
	if !strings.Contains(strip(lines[4]), "…") { // the long row truncated (only two row lines exist ⇒ no wrap)
		t.Errorf("long row was not truncated: %q", strip(lines[4]))
	}
	if !strings.Contains(strip(lines[5]), "esc cancel") {
		t.Errorf("hint is not the last content row: %q", strip(lines[5]))
	}

	noBody := spec
	noBody.body = ""
	if got := len(popupLines(renderPopup(th, noBody, width))); got != len(lines)-1 {
		t.Errorf("empty body changed the row count by other than one: %d vs %d", got, len(lines)-1)
	}
}

// The body renders in its own (non-faint) style: a body line does NOT carry the faint chrome SGR
// the hint line does, so the two read as distinct tiers of the hierarchy (title bold / body normal
// / chrome faint).
func TestRenderPopupBodyIsNotFaint(t *testing.T) {
	th := newTheme()
	lines := popupLines(renderPopup(th, popupSpec{body: "body text here", hint: "esc cancel"}, 50))
	bodyLine, hintLine := lines[1], lines[2] // borders + body + hint

	faint := th.statusFaint.Render("x")
	idx := strings.IndexByte(faint, 'm')
	if idx < 0 {
		t.Skip("no colour profile in this environment — the faint-SGR distinction is not observable")
	}
	faintSGR := faint[:idx+1]
	if !strings.Contains(hintLine, faintSGR) {
		t.Fatalf("hint line lacks the faint SGR %q — test premise broken", faintSGR)
	}
	if strings.Contains(bodyLine, faintSGR) {
		t.Errorf("body line carries the hint's faint SGR %q; the body must render in a distinct style", faintSGR)
	}
}

// With a body present, a degenerate width still degrades gracefully: a width ≤ the border frame
// renders nothing, and an inner width of 1 neither panics nor produces a line wider than the box
// (the wrapped body and the overflow marker both clip to the single inner cell).
func TestRenderPopupBodyDegenerateWidth(t *testing.T) {
	th := newTheme()
	frame := th.popupBorder.GetHorizontalFrameSize()
	spec := popupSpec{
		title:       "approve run?",
		body:        strings.Repeat("reason ", 30),
		maxBodyRows: 4,
		hint:        "esc cancel",
	}
	if out := renderPopup(th, spec, frame); out != "" {
		t.Errorf("width == frame should render nothing, got %q", strip(out))
	}
	out := renderPopup(th, spec, frame+1) // inner width 1 — must not panic
	for i, ln := range popupLines(out) {
		if w := lipgloss.Width(ln); w > frame+1 {
			t.Errorf("inner-width-1: line %d is %d cells, exceeds width %d: %q", i, w, frame+1, strip(ln))
		}
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
