package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Mouse: click-to-position + drag-to-select in the prompt (mouse.go)
// ----------------------------------------------------------------------------

// modelWithInput builds a ready idle model whose prompt already holds value, laid out so the
// input box height and the content rectangle are settled before any mouse event.
func modelWithInput(t *testing.T, value string) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.input.SetValue(value)
	m.layout()
	return m
}

// click/drag/release Msg constructors at an absolute screen cell with the left button.
func leftClick(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}
func leftDrag(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
}
func leftRelease(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func TestCaretOffset(t *testing.T) {
	cases := []struct {
		name       string
		value      string
		row, col   int
		wantOffset int
	}{
		{"start", "hello world", 0, 0, 0},
		{"midline", "hello world", 0, 6, 6},
		{"end", "hello world", 0, 11, 11},
		{"second line counts the newline", "ab\ncd", 1, 1, 4},
		{"second line start", "ab\ncd", 1, 0, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := caretOffset(c.value, c.row, c.col); got != c.wantOffset {
				t.Fatalf("caretOffset(%q,%d,%d) = %d, want %d", c.value, c.row, c.col, got, c.wantOffset)
			}
			// The offset must index []rune(value) directly — newlines preserved, soft-wraps absent.
			if r := []rune(c.value); c.wantOffset <= len(r) {
				_ = string(r[:c.wantOffset]) // must not panic
			}
		})
	}
}

// offsetToLineCol must invert caretOffset at EVERY position of a value, line ends and multi-byte
// runes included — a completion that splices mid-draft computes its new caret as an offset and can
// only drive the widget by row and column, so a single off-by-one there would drop the caret inside
// a rune. The byte↔rune bridge the mini-language crosses to reach those offsets is pinned with it.
func TestCaretOffsetRoundTrips(t *testing.T) {
	values := []string{
		"",
		"hello world",
		"ab\ncd",
		"first line\n\nthird line\n",
		"日本語のテキスト\n絵文字 🚀 も",
		"/grill-me 見て @internal/tui/model.go",
	}
	for _, v := range values {
		t.Run(v, func(t *testing.T) {
			for off := 0; off <= len([]rune(v)); off++ {
				row, col := offsetToLineCol(v, off)
				if got := caretOffset(v, row, col); got != off {
					t.Fatalf("offsetToLineCol(%q, %d) = (%d,%d), which caretOffset reads back as %d", v, off, row, col, got)
				}
				if got := runeOffsetOf(v, byteOffsetOf(v, off)); got != off {
					t.Fatalf("runeOffsetOf(byteOffsetOf(%q, %d)) = %d, want %d", v, off, got, off)
				}
			}
			// Out-of-range offsets clamp to the value's two ends rather than panicking.
			if row, col := offsetToLineCol(v, -1); row != 0 || col != 0 {
				t.Errorf("offsetToLineCol(%q, -1) = (%d,%d), want the first position", v, row, col)
			}
			if got, want := byteOffsetOf(v, len([]rune(v))+9), len(v); got != want {
				t.Errorf("byteOffsetOf(%q, past the end) = %d, want %d", v, got, want)
			}
		})
	}
}

func TestSelectionText(t *testing.T) {
	v := "hello\nworld"
	cases := []struct {
		name string
		a, b int
		want string
	}{
		{"forward", 0, 5, "hello"},
		{"reversed gives same span", 5, 0, "hello"},
		{"across the newline", 0, 7, "hello\nw"},
		{"clamped high", 0, 999, "hello\nworld"},
		{"clamped low", -3, 5, "hello"},
		{"empty", 4, 4, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := selectionText(v, c.a, c.b); got != c.want {
				t.Fatalf("selectionText(%q,%d,%d) = %q, want %q", v, c.a, c.b, got, c.want)
			}
		})
	}
}

// TestClickPositionsCaret feeds a click through Update and checks the caret landed on the
// clicked character. The content rectangle for an 80x24, single-row prompt starts at x=2
// (border + padding) and the text row sits at y = height - footerHeight - 1 = 20.
func TestClickPositionsCaret(t *testing.T) {
	m := modelWithInput(t, "hello world")
	const textRowY = 24 - footerHeight - 1 // single content row, bottom-anchored above the footer

	m = step(t, m, leftClick(2+6, textRowY)) // x0(2) + column 6 → the 'w'
	if got := m.input.Line(); got != 0 {
		t.Fatalf("Line() = %d, want 0", got)
	}
	if got := m.input.Column(); got != 6 {
		t.Fatalf("Column() = %d, want 6 (the 'w' in 'hello world')", got)
	}
	if !m.sel.active || m.sel.anchorOff != 6 || m.sel.headOff != 6 {
		t.Fatalf("a bare click should arm a collapsed selection at offset 6, got %+v", m.sel)
	}
}

// TestClickPositionsCaretMultiline checks row mapping (and the +1-per-newline offset) on a
// two-row prompt.
func TestClickPositionsCaretMultiline(t *testing.T) {
	m := modelWithInput(t, "ab\ncd")
	// Two content rows, bottom-anchored: row 1 ("cd") sits at y = height - footerHeight - 1.
	const row1Y = 24 - footerHeight - 1

	m = step(t, m, leftClick(2+1, row1Y)) // x0(2) + column 1 → the 'd'
	if m.input.Line() != 1 || m.input.Column() != 1 {
		t.Fatalf("caret at row %d col %d, want row 1 col 1", m.input.Line(), m.input.Column())
	}
	if m.sel.anchorOff != 4 { // a(0) b(1) \n(2) c(3) d(4)
		t.Fatalf("anchorOff = %d, want 4", m.sel.anchorOff)
	}
}

// TestDragSelectsAndCopies drives press → drag → release and checks the selection span, the
// copy Cmd, and the confirmation flash.
func TestDragSelectsAndCopies(t *testing.T) {
	m := modelWithInput(t, "hello world")
	const y = 24 - footerHeight - 1

	m = step(t, m, leftClick(2+0, y)) // anchor at column 0
	m = step(t, m, leftDrag(2+5, y))  // drag head to column 5 → "hello"
	if m.sel.anchorOff != 0 || m.sel.headOff != 5 {
		t.Fatalf("selection offsets = (%d,%d), want (0,5)", m.sel.anchorOff, m.sel.headOff)
	}
	if got := selectionText(m.input.Value(), m.sel.anchorOff, m.sel.headOff); got != "hello" {
		t.Fatalf("selected text = %q, want %q", got, "hello")
	}

	m, cmd := stepCmd(t, m, leftRelease(2+5, y))
	if cmd == nil {
		t.Fatal("release of a non-empty selection should return a copy Cmd, got nil")
	}
	if !strings.Contains(m.flash, "copied 5 chars") {
		t.Fatalf("flash = %q, want it to mention 'copied 5 chars'", m.flash)
	}
}

// TestBareClickReleaseDoesNotCopy ensures a click without a drag leaves the caret but copies
// nothing (no flash, no Cmd) and collapses the selection.
func TestBareClickReleaseDoesNotCopy(t *testing.T) {
	m := modelWithInput(t, "hello world")
	const y = 24 - footerHeight - 1

	m = step(t, m, leftClick(2+3, y))
	m, cmd := stepCmd(t, m, leftRelease(2+3, y))
	if cmd != nil {
		t.Fatal("a bare click+release should not copy, got a Cmd")
	}
	if m.flash != "" {
		t.Fatalf("flash = %q, want empty after a bare click", m.flash)
	}
	if m.sel.active {
		t.Fatal("a bare click+release should collapse the selection")
	}
}

// TestClickOffFieldDeselects checks that clicking outside the text rows clears a selection.
func TestClickOffFieldDeselects(t *testing.T) {
	m := modelWithInput(t, "hello world")
	m.sel = promptSel{active: true, anchorOff: 0, headOff: 5}

	m = step(t, m, leftClick(5, 0)) // y=0 is the transcript, well above the input box
	if m.sel.active {
		t.Fatal("a click off the prompt should clear the selection")
	}
}

// TestClickPositionsCaretWhileRunning replaces TestClickIgnoredWhileRunning: the prompt is
// editable while a worker runs (the human is typing an interjection into it), so a click there
// positions the caret and arms a prompt selection exactly as it does at idle — the prompt half of
// "select at any point in time" (ADR 0025; the plan's decision 9).
func TestClickPositionsCaretWhileRunning(t *testing.T) {
	m := modelWithInput(t, "hello world")
	m.state = stateRunning
	m.input.MoveToEnd()

	m = step(t, m, leftClick(2+0, 24-footerHeight-1)) // column 0 of the single content row
	if got := m.input.Column(); got != 0 {
		t.Fatalf("click did not position the caret while running (col %d, want 0)", got)
	}
	if !m.sel.active || m.sel.anchorOff != 0 || m.sel.headOff != 0 {
		t.Fatalf("a click while running should arm a collapsed selection at offset 0, got %+v", m.sel)
	}
}

// TestKeypressClearsSelection checks the single chokepoint in handleKey drops a live selection.
func TestKeypressClearsSelection(t *testing.T) {
	m := modelWithInput(t, "hello world")
	m.sel = promptSel{active: true, anchorOff: 0, headOff: 5}

	m = step(t, m, tea.KeyPressMsg{Code: 'x'})
	if m.sel.active {
		t.Fatal("a keypress should clear the mouse selection")
	}
}

// TestShadeCellsPreservesGlyphs checks that shading a cell range neither adds nor drops visible
// characters — only styling changes.
func TestShadeCellsPreservesGlyphs(t *testing.T) {
	const line = "hello world"
	th := newTestModel(t).th
	out := shadeCells(th.measure, line, 2, 5, th.selection)
	if got := ansi.Strip(out); got != line {
		t.Fatalf("shadeCells changed the glyphs: %q, want %q", got, line)
	}
}

// TestHighlightInputPreservesGlyphs checks the rendered prompt block keeps its text when a
// selection is overlaid (the highlight is styling-only).
func TestHighlightInputPreservesGlyphs(t *testing.T) {
	m := modelWithInput(t, "hello world")
	m.sel = promptSel{
		active:    true,
		anchorOff: 0, headOff: 5,
		anchorVis: cell{0, 0}, headVis: cell{0, 5},
	}
	view := m.input.View()
	if got, want := ansi.Strip(m.highlightInput(view)), ansi.Strip(view); got != want {
		t.Fatalf("highlightInput changed the glyphs:\n got %q\nwant %q", got, want)
	}
}

// selectionBg is the truecolor SGR for colSelection (#3a5fcd → 58,95,205), the marker that the
// selection background actually reached the rendered output.
const selectionBg = "48;2;58;95;205"

// TestViewRendersSelectionHighlight drives a full drag through Update and confirms the
// selection background appears in the whole-screen View — end-to-end, not just the helper.
func TestViewRendersSelectionHighlight(t *testing.T) {
	m := modelWithInput(t, "hello world")
	const y = 24 - footerHeight - 1
	m = step(t, m, leftClick(2+0, y))
	m = step(t, m, leftDrag(2+5, y))

	if before := newTestModel(t).View().Content; strings.Contains(before, selectionBg) {
		t.Fatal("the selection colour must not appear without a selection")
	}
	if got := m.View().Content; !strings.Contains(got, selectionBg) {
		t.Fatal("active selection did not reach the rendered View (no highlight background)")
	}
}

// ----------------------------------------------------------------------------
// Cell-vs-rune caret mapping: clicks/drags land on the right rune with wide glyphs
// ----------------------------------------------------------------------------

// TestCellToRuneOffset pins the conversion at the heart of the caret fix: a display-cell column
// maps to a rune offset, a column inside a wide rune resolves to that rune's left edge, and a
// column past the run clamps to the rune count (not the cell count).
func TestCellToRuneOffset(t *testing.T) {
	cases := []struct {
		name  string
		value string
		cells int
		want  int
	}{
		{"ascii midline", "hello", 3, 3},
		{"ascii clamps to rune count", "hi", 10, 2},
		{"zero cells", "abc", 0, 0},
		{"empty run", "", 5, 0},
		{"cjk start of 2nd glyph", "日本語", 2, 1}, // each Han rune is 2 cells wide
		{"cjk start of 3rd glyph", "日本語", 4, 2},
		{"cjk end", "日本語", 6, 3},
		{"cjk inside wide rune → left edge", "日本語", 5, 2},
		{"mixed: first ascii after the cjk run", "日本語 text", 7, 4}, // 6 cells cjk + 1 space, then 't'
		{"mixed clamps past end", "日本語 text", 999, 8},
		// The widget measures "⚠️" as one two-cell grapheme (uniseg), so cell 3 is the 'b' after
		// it — a per-rune ruler reads U+26A0 as one cell and U+FE0F as none and lands on 'b' a
		// cell early, taking the caret with it.
		{"vs16 cluster is two cells wide", "a⚠️b", 3, 3},
		{"vs16 end", "a⚠️b", 4, 4},
		{"vs16 clamps past end", "a⚠️b", 99, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cellToRuneOffset([]rune(c.value), c.cells); got != c.want {
				t.Fatalf("cellToRuneOffset(%q, %d) = %d, want %d", c.value, c.cells, got, c.want)
			}
		})
	}
}

// TestCellToRuneOffsetInvertsWidth is the invariant the caret relies on: at every rune boundary,
// the offset that renders at that boundary's cumulative cell width maps back to that same
// boundary — for any script.
//
// The oracle is the widget's own cursor math, so the cumulative width is uniseg.StringWidth of the
// prefix, exactly as textarea.LineInfo computes CharOffset and textarea.Cursor its x. Reaching for
// the library directly rather than for runesWidth keeps this a check against the widget instead of
// a check of the mirror against itself. The "⚠️" fixture is the one a per-rune ruler fails: it
// reads the cluster as one cell where the widget reads two, so every boundary after it maps back
// short. The invariant holds only for prefixes whose width strictly grows, so the fixtures avoid
// combining marks.
func TestCellToRuneOffsetInvertsWidth(t *testing.T) {
	for _, s := range []string{"hello", "日本語 text", "aあb🙂c", "a⚠️b ⚠️", ""} {
		runes := []rune(s)
		for k := 0; k <= len(runes); k++ {
			acc := uniseg.StringWidth(string(runes[:k]))
			if got := cellToRuneOffset(runes, acc); got != k {
				t.Errorf("%q: cellToRuneOffset(., %d cells) = %d, want boundary %d", s, acc, got, k)
			}
		}
	}
}

// TestVisualSubline checks the sub-line slice caretTo feeds the cell→rune conversion: it returns
// exactly the [start, start+width) runes of the row-th logical line, bounds a wrapped row so a
// click near the wrap point cannot read into the next visual row, and clamps out-of-range inputs
// to an empty slice.
func TestVisualSubline(t *testing.T) {
	cases := []struct {
		name              string
		value             string
		row, start, width int
		want              string
	}{
		{"whole unwrapped line", "hello", 0, 0, 5, "hello"},
		{"second logical line", "ab\ncd", 1, 0, 2, "cd"},
		{"wrapped row starts mid-line, bounded", "abcdef", 0, 3, 3, "def"},
		{"width clamps to line end", "abc", 0, 1, 99, "bc"},
		{"row out of range → empty", "abc", 5, 0, 3, ""},
		{"start past end → empty", "abc", 0, 10, 2, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(visualSubline(c.value, c.row, c.start, c.width)); got != c.want {
				t.Fatalf("visualSubline(%q, %d, %d, %d) = %q, want %q", c.value, c.row, c.start, c.width, got, c.want)
			}
		})
	}
}

// TestClickPositionsCaretCJK is the end-to-end regression for the caret fix: a click at a display
// column on a line of wide glyphs lands the caret on the rune under that column, not the rune at
// that column's numeric value (offset 4 = 't' under the buggy cell-as-rune path).
func TestClickPositionsCaretCJK(t *testing.T) {
	m := modelWithInput(t, "日本語 text")
	const y = 24 - footerHeight - 1

	m = step(t, m, leftClick(2+4, y)) // display cell 4 = the start of 語
	if m.input.Line() != 0 || m.input.Column() != 2 {
		t.Fatalf("caret at row %d col %d, want row 0 col 2 (the rune 語)", m.input.Line(), m.input.Column())
	}
	if !m.sel.active || m.sel.anchorOff != 2 {
		t.Fatalf("click should arm a collapsed selection at rune offset 2, got %+v", m.sel)
	}
}

// TestDragCopyCJKMatchesHighlight is the clipboard-vs-highlight regression: a drag over wide
// glyphs must copy the runes actually under the highlighted cells. Dragging cells [0,6) over
// "日本語 text" highlights the three Han glyphs, so the clipboard must hold exactly "日本語" —
// the buggy path copied "日本語 te" (six runes, treating the cell span as a rune span).
func TestDragCopyCJKMatchesHighlight(t *testing.T) {
	m := modelWithInput(t, "日本語 text")
	const y = 24 - footerHeight - 1

	m = step(t, m, leftClick(2+0, y)) // anchor at cell 0
	m = step(t, m, leftDrag(2+6, y))  // head at cell 6 → the three wide glyphs
	if m.sel.anchorOff != 0 || m.sel.headOff != 3 {
		t.Fatalf("selection rune offsets = (%d,%d), want (0,3)", m.sel.anchorOff, m.sel.headOff)
	}
	if got := selectionText(m.input.Value(), m.sel.anchorOff, m.sel.headOff); got != "日本語" {
		t.Fatalf("copied text = %q, want %q (must match the highlighted cells)", got, "日本語")
	}

	m, cmd := stepCmd(t, m, leftRelease(2+6, y))
	if cmd == nil {
		t.Fatal("release of a non-empty selection should return a copy Cmd, got nil")
	}
	if !strings.Contains(m.flash, "copied 3 chars") {
		t.Fatalf("flash = %q, want 'copied 3 chars'", m.flash)
	}
}

// TestDragCopyAcrossSoftWrap drives a click and drag on the second visual row of a soft-wrapped
// logical line and checks the copied runes are the ones under the cells. The wrap width is
// discovered at runtime (a max-x click lands the caret at the end of row 0), so the test does not
// hard-code the textarea's wrap column.
func TestDragCopyAcrossSoftWrap(t *testing.T) {
	// One logical line (no '\n') long enough to wrap; a distinctive tail makes the copied slice
	// unambiguous.
	value := strings.Repeat("a", 90) + "0123456789tail"
	m := modelWithInput(t, value)

	x0, y0, w, h := m.inputContentRect()
	if h < 2 {
		t.Fatalf("value did not wrap: box height %d, want ≥2 visual rows", h)
	}

	// Calibrate: a click past the right edge of row 0 clamps to the row end; Column() is then the
	// rune count of the first visual sub-line (the wrap width).
	m = step(t, m, leftClick(x0+w, y0))
	wrap := m.input.Column()
	if wrap <= 0 || wrap >= len([]rune(value)) {
		t.Fatalf("calibrated wrap width = %d, want a genuine soft wrap", wrap)
	}

	// Now click+drag on row 1 (the last visual row, at y0+1): cells [3,8) → rune offsets
	// [wrap+3, wrap+8) of the single logical line.
	row1Y := y0 + 1
	m = step(t, m, leftClick(x0+3, row1Y))
	if got, want := m.input.Column(), wrap+3; got != want {
		t.Fatalf("caret column after row-1 click = %d, want %d (wrap %d + 3)", got, want, wrap)
	}
	m = step(t, m, leftDrag(x0+8, row1Y))
	if m.sel.anchorOff != wrap+3 || m.sel.headOff != wrap+8 {
		t.Fatalf("selection offsets = (%d,%d), want (%d,%d)", m.sel.anchorOff, m.sel.headOff, wrap+3, wrap+8)
	}
	runes := []rune(value)
	want := string(runes[wrap+3 : wrap+8])
	if got := selectionText(value, m.sel.anchorOff, m.sel.headOff); got != want {
		t.Fatalf("copied text = %q, want %q (the runes under cells [3,8) of row 1)", got, want)
	}
}

// ----------------------------------------------------------------------------
// Bracketed paste runs the same edit path as a keypress (model.go Update)
// ----------------------------------------------------------------------------

// TestPasteInsertsAndRefreshes checks a PasteMsg inserts the content, drops any live selection,
// and runs layout() — the box grows to fit a multi-line paste, which the buggy default-case path
// deferred until the next keypress.
func TestPasteInsertsAndRefreshes(t *testing.T) {
	m := modelWithInput(t, "")
	before := m.input.Height()
	m.sel = promptSel{active: true, anchorOff: 0, headOff: 3} // a stale selection to be dropped

	m = step(t, m, tea.PasteMsg{Content: "line1\nline2\nline3"})

	if got := m.input.Value(); got != "line1\nline2\nline3" {
		t.Fatalf("paste did not insert: value = %q", got)
	}
	if m.sel.active {
		t.Fatal("paste should clear the live selection before its coords go stale")
	}
	if m.input.Height() <= before {
		t.Fatalf("box did not grow for the multi-line paste (layout ran?): before %d, after %d", before, m.input.Height())
	}
}

// TestPasteRecomputesAutocomplete checks the paste path re-derives the autocomplete overlay: a
// pasted "/comp" opens the command overlay exactly as typing it would.
func TestPasteRecomputesAutocomplete(t *testing.T) {
	m := modelWithInput(t, "")
	m = step(t, m, tea.PasteMsg{Content: "/comp"})
	if !m.autocomplete.active || m.autocomplete.kind != acCommand {
		t.Fatalf("paste did not recompute the command autocomplete: %+v", m.autocomplete)
	}
}

// TestPasteIgnoredWhereInputIsInert checks a paste is dropped in the states that refuse keypress
// edits too — an approval decision and the errored dismiss own the keyboard there. The running
// state is deliberately NOT among them any more: TestPasteWhileRunningTypes (interject_test.go)
// replaced TestPasteIgnoredWhileRunning when typing while the model works landed (ADR 0025).
func TestPasteIgnoredWhereInputIsInert(t *testing.T) {
	for _, state := range []uiState{stateAwaitingApproval, stateErrored} {
		m := modelWithInput(t, "keep")
		m.state = state
		m = step(t, m, tea.PasteMsg{Content: "junk"})
		if got := m.input.Value(); got != "keep" {
			t.Fatalf("paste at state %v must not edit the input, got %q", state, got)
		}
	}
}

// ----------------------------------------------------------------------------
// Transcript: screen-space drag-to-select-to-copy in the viewport (mouse.go)
// ----------------------------------------------------------------------------

// modelWithTranscript builds a ready idle model whose transcript holds a single user prompt,
// rendered into m.lines and laid out so the viewport rectangle is settled before any mouse event.
func modelWithTranscript(t *testing.T, prompt string) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.transcript.addUser(prompt, nil)
	m.refreshViewport()
	return m
}

// TestTranscriptSelectionText is the extraction math: it slices display-cell ranges out of fake
// rendered lines (ANSI-styled, wide glyphs, a blank between-blocks line, trailing pad) and checks
// the plain text copied — trailing pad trimmed, styling stripped, reading order normalised.
func TestTranscriptSelectionText(t *testing.T) {
	sty := lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	lines := []string{
		sty.Render("hello world") + "     ", // ANSI-styled content with 5 cells of trailing pad
		"日本語 text",                          // wide (2-cell) glyphs
		"",                                  // a blank between-blocks line
		"tail",
	}
	// Every glyph in the fixtures measures the same under both width methods, so the extraction
	// math is the only thing under test here; the case the two measures disagree about has its
	// own painted test (TestTranscriptClickSelectsThePaintedGlyph).
	measure := newWidthAuthority()
	cases := []struct {
		name string
		a, b contentCell
		want string
	}{
		{"single line trims trailing pad", contentCell{0, 0}, contentCell{0, 20}, "hello world"},
		{"reversed span reads the same", contentCell{0, 20}, contentCell{0, 0}, "hello world"},
		{"mid-line cut", contentCell{0, 0}, contentCell{0, 5}, "hello"},
		{"wide glyphs by display cell", contentCell{1, 0}, contentCell{1, 6}, "日本語"},
		{"multi-line spans to the last cut", contentCell{0, 0}, contentCell{1, 6}, "hello world\n日本語"},
		{"blank line preserved across blocks", contentCell{0, 0}, contentCell{3, 4}, "hello world\n日本語 text\n\ntail"},
		{"rows past the end are clamped", contentCell{3, 0}, contentCell{9, 9}, "tail"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := transcriptSelectionText(measure, lines, c.a, c.b)
			if got != c.want {
				t.Fatalf("transcriptSelectionText(%+v,%+v) = %q, want %q", c.a, c.b, got, c.want)
			}
			if strings.Contains(got, "\x1b") {
				t.Fatalf("copied text still carries ANSI escapes: %q", got)
			}
		})
	}
}

// TestTranscriptDragSelectsAndCopies drives a click → drag → release over the rendered prompt row
// and checks the extracted plain text, the copy Cmd, and the confirmation flash.
func TestTranscriptDragSelectsAndCopies(t *testing.T) {
	m := modelWithTranscript(t, "hello world")
	w := m.viewport.Width()

	m = step(t, m, leftClick(0, 0)) // anchor at the row's first cell (the viewport is top-anchored)
	m = step(t, m, leftDrag(w, 0))  // drag past the right edge → the whole row
	if !m.transcriptSel.active {
		t.Fatal("a transcript drag did not arm a selection")
	}
	got := transcriptSelectionText(m.th.measure, m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
	if want := glyphUser + " hello world"; got != want {
		t.Fatalf("selected text = %q, want %q (the rendered user block, pad trimmed)", got, want)
	}

	m, cmd := stepCmd(t, m, leftRelease(w, 0))
	if cmd == nil {
		t.Fatal("release of a non-empty transcript selection should return a copy Cmd, got nil")
	}
	if !strings.Contains(m.flash, "copied") {
		t.Fatalf("flash = %q, want a copy confirmation", m.flash)
	}
}

// TestTranscriptBareClickCopiesNothing checks a click without a drag copies nothing (no flash, no
// Cmd) and collapses the selection — the same bare-click rule the prompt follows.
func TestTranscriptBareClickCopiesNothing(t *testing.T) {
	m := modelWithTranscript(t, "hello world")

	m = step(t, m, leftClick(2, 0))
	m, cmd := stepCmd(t, m, leftRelease(2, 0))
	if cmd != nil {
		t.Fatal("a bare transcript click+release should not copy, got a Cmd")
	}
	if m.flash != "" {
		t.Fatalf("flash = %q, want empty after a bare click", m.flash)
	}
	if m.transcriptSel.active {
		t.Fatal("a bare transcript click+release should collapse the selection")
	}
}

// TestTranscriptSelectionSurvivesWheelScroll checks the content-anchored selection is untouched by
// a mid-drag wheel scroll: the anchor names a content line, not a screen row, so scrolling moves
// what is on screen without moving (or clearing) the selection.
func TestTranscriptSelectionSurvivesWheelScroll(t *testing.T) {
	m := newTestModel(t)
	m.transcript.addUser("top prompt", nil)
	for i := 0; i < 40; i++ {
		m.transcript.commitAssistant("reply "+strings.Repeat("x", 5), 0)
	}
	m.refreshViewport()
	m.viewport.GotoBottom() // scroll down so there is room to wheel back up

	m = step(t, m, leftClick(0, 0)) // start a selection on the top visible row
	m = step(t, m, leftDrag(3, 0))
	if !m.transcriptSel.active {
		t.Fatal("precondition: no transcript selection armed")
	}
	anchor := m.transcriptSel.anchor

	before := m.viewport.YOffset()
	m = step(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.viewport.YOffset() == before {
		t.Fatal("precondition: wheel-up did not scroll the viewport")
	}
	if !m.transcriptSel.active {
		t.Fatal("a wheel-scroll cleared the transcript selection")
	}
	if m.transcriptSel.anchor != anchor {
		t.Fatalf("wheel-scroll moved the content-anchored anchor: %+v → %+v", anchor, m.transcriptSel.anchor)
	}
}

// ----------------------------------------------------------------------------
// The pointer, the highlight and the clipboard agree — in PAINTED columns (mouse.go, width.go)
// ----------------------------------------------------------------------------
//
// A mouse report names a painted cell. Everything the selection does with that number — cutting
// the copied text, shading the highlight — has to speak the same measure, which is the width
// authority's whole job (width.go). These two tests assert that at the painted layer, on the one
// grapheme the two measures disagree about, under both of them.

// vs16TranscriptRow builds a model whose transcript carries a ⚠️ row, moves its width authority to
// method (the state the real program is in once the terminal has answered mode 2027, or has not),
// and returns it with the painted row that carries the grapheme and the painted column target
// starts at on it. Locating the column by PAINT rather than by counting runes is the point: it is
// the coordinate the terminal would report for a click on that word.
func vs16TranscriptRow(t *testing.T, method ansi.Method, target string) (m Model, row, col int) {
	t.Helper()
	m = paintedAs(t, modelWithTranscript(t, "danger "+vs16Warning+" "+target), method)
	for y, painted := range transcriptPaintRows(t, m, method) {
		if !strings.Contains(painted, vs16Warning) {
			continue
		}
		col = paintedColumn(painted, target, method)
		if col < 0 {
			t.Fatalf("painted row %d carries the ⚠️ but not %q: %q", y, target, painted)
		}
		return m, y, col
	}
	t.Fatal("no painted transcript row carries the ⚠️ — the fixture no longer exercises the drift")
	return m, 0, 0
}

// TestTranscriptClickSelectsThePaintedGlyph is symptom 2's regression: a click at painted column n
// selects the glyph the terminal painted at column n, on a row carrying the disputed grapheme.
//
// Before the authority the cut ran in GraphemeWidth whatever the painter was doing, so on a
// WcWidth painter — every terminal that does not answer mode 2027 — the ⚠️ ate a column the
// terminal never drew, and every column past it named the glyph one cell to its left.
func TestTranscriptClickSelectsThePaintedGlyph(t *testing.T) {
	const target = "zebra" // one occurrence on the row, so the painted column is unambiguous
	for _, tc := range paintMethods {
		t.Run(tc.name, func(t *testing.T) {
			m, row, col := vs16TranscriptRow(t, tc.method, target)

			m = step(t, m, leftClick(col, row))
			m = step(t, m, leftDrag(col+paintedWidth(target, tc.method), row))

			got := transcriptSelectionText(m.th.measure, m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
			if got != target {
				t.Fatalf("a drag from painted column %d copied %q, want the %q painted there", col, got, target)
			}
		})
	}
}

// TestTranscriptHighlightExtentMatchesTheCopy is the third party to the agreement: what the human
// SEES selected is exactly what the clipboard took. Both are measured against the span the drag
// described in painted columns, so a highlight that shaded a cell more or less than the copy — the
// old failure mode, where highlight and clipboard agreed with each other and disagreed with the
// pointer — fails here.
func TestTranscriptHighlightExtentMatchesTheCopy(t *testing.T) {
	const target = "zebra"
	for _, tc := range paintMethods {
		t.Run(tc.name, func(t *testing.T) {
			m, row, col := vs16TranscriptRow(t, tc.method, target)
			end := col + paintedWidth(target, tc.method) // drag the whole row up to the target's end

			m = step(t, m, leftClick(0, row))
			m = step(t, m, leftDrag(end, row))

			text := transcriptSelectionText(m.th.measure, m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
			if got := m.th.measure.Width(text); got != end {
				t.Errorf("the copied text paints %d columns, want the %d the drag covered: %q", got, end, text)
			}
			if got := m.th.measure.Width(shadedRun(t, m.View().Content)); got != end {
				t.Errorf("the highlight paints %d columns, want the %d the drag covered", got, end)
			}
		})
	}
}

// shadedRun returns the glyphs the selection style covers in a rendered frame: the run between the
// SGR sequence that carries selectionBg and the reset that closes it. shadeCells strips the span it
// re-renders, so the run holds no escapes of its own and its width is the highlight's painted extent.
func shadedRun(t *testing.T, frame string) string {
	t.Helper()
	i := strings.Index(frame, selectionBg)
	if i < 0 {
		t.Fatal("the frame carries no selection highlight")
	}
	open := strings.IndexByte(frame[i:], 'm') // the terminator of the SGR that opens the run
	if open < 0 {
		t.Fatalf("unterminated SGR sequence at the highlight: %q", frame[i:min(i+32, len(frame))])
	}
	run := frame[i+open+1:]
	if end := strings.IndexByte(run, '\x1b'); end >= 0 {
		run = run[:end]
	}
	return run
}

// ----------------------------------------------------------------------------
// Keep-if-unchanged: a transcript selection survives the stream (mouse.go, refreshViewport)
// ----------------------------------------------------------------------------

// screenRow maps a rendered content line onto the viewport row it is drawn on, so a test can aim
// the mouse at a line it located by CONTENT rather than by guessing at the scroll position.
func screenRow(t *testing.T, m Model, line int) int {
	t.Helper()
	row := line - m.viewport.YOffset()
	if row < 0 || row >= m.viewport.Height() {
		t.Fatalf("content line %d is off screen (offset %d, height %d)", line, m.viewport.YOffset(), m.viewport.Height())
	}
	return row
}

// armTranscriptSelection drags across the settled user block — on the viewport's top row, because a
// transcript this short pads out to put the followed bottom on the prompt row — and returns the
// model with that selection live.
func armTranscriptSelection(t *testing.T, m Model) Model {
	t.Helper()
	m = step(t, m, leftClick(0, 0))
	m = step(t, m, leftDrag(m.viewport.Width(), 0))
	if !m.transcriptSel.active {
		t.Fatal("precondition: no transcript selection armed")
	}
	return m
}

// TestTranscriptSelectionSurvivesStreamAppend is the keep-if-unchanged rule's headline case: a
// selection over settled text lives through the repaint every streamed token causes, stays
// highlighted while the reply grows beneath it, and copies exactly the text still on screen. It
// replaces TestTranscriptSelectionClearsOnStreamToken, which pinned the old clear-on-every-repaint
// behaviour a drag could not survive.
func TestTranscriptSelectionSurvivesStreamAppend(t *testing.T) {
	m := modelWithTranscript(t, "hello world")
	m = armTranscriptSelection(t, m)

	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "a streamed reply"}})
	if !m.transcriptSel.active {
		t.Fatal("a stream token dropped a selection over settled lines")
	}
	if !strings.Contains(m.View().Content, selectionBg) {
		t.Fatal("the kept selection is no longer highlighted in the View")
	}

	m, cmd := stepCmd(t, m, leftRelease(m.viewport.Width(), 0))
	if cmd == nil {
		t.Fatal("release of the kept selection should return a copy Cmd, got nil")
	}
	got := transcriptSelectionText(m.th.measure, m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
	if want := glyphUser + " hello world"; got != want {
		t.Fatalf("copied %q, want %q — a kept selection must copy what is on screen", got, want)
	}
}

// TestTranscriptSelectionDropsWhenSpanChanges is the rule's other half: a selection over the
// still-moving streaming tail drops the moment the next token rewrites those lines — no highlight
// left behind, and the release copies nothing.
func TestTranscriptSelectionDropsWhenSpanChanges(t *testing.T) {
	m := modelWithTranscript(t, "hello world")
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "half a"}})
	row := screenRow(t, m, len(m.lines)-1) // the in-progress assistant buffer's last line

	m = step(t, m, leftClick(0, row))
	m = step(t, m, leftDrag(6, row))
	if !m.transcriptSel.active {
		t.Fatal("precondition: no transcript selection armed over the streaming tail")
	}

	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: " reply"}})
	if m.transcriptSel.active {
		t.Fatal("a selection whose own lines were rewritten must drop")
	}
	if strings.Contains(m.View().Content, selectionBg) {
		t.Fatal("a dropped selection is still highlighted in the View")
	}

	m, cmd := stepCmd(t, m, leftRelease(6, row))
	if cmd != nil {
		t.Fatal("releasing a dropped selection should copy nothing, got a Cmd")
	}
	if m.flash != "" {
		t.Fatalf("flash = %q, want empty after a dropped selection", m.flash)
	}
}

// TestTranscriptMidDragSurvivesRepaint checks the drag itself lives through a repaint: motion,
// a streamed token folded mid-drag, more motion, release — and the copy is the settled span, so
// the drag never died and never lost its anchor.
func TestTranscriptMidDragSurvivesRepaint(t *testing.T) {
	m := modelWithTranscript(t, "hello world")
	w := m.viewport.Width()

	m = step(t, m, leftClick(0, 0))
	m = step(t, m, leftDrag(5, 0))
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "tokens landing mid-drag"}})
	if !m.transcriptSel.active {
		t.Fatal("a repaint between two drag motions killed the drag")
	}

	m = step(t, m, leftDrag(w, 0)) // the drag carries on to the end of the row
	m, cmd := stepCmd(t, m, leftRelease(w, 0))
	if cmd == nil {
		t.Fatal("release after a surviving drag should return a copy Cmd, got nil")
	}
	got := transcriptSelectionText(m.th.measure, m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
	if want := glyphUser + " hello world"; got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
}

// TestTranscriptSelectionResize checks the rule against the two resizes: a width change rewraps
// the lines under the span, so the selection drops; a height-only change re-renders to identical
// lines, so it is kept. It replaces TestTranscriptSelectionClearsOnResize, which could not tell
// the two apart because every repaint cleared.
func TestTranscriptSelectionResize(t *testing.T) {
	t.Run("a width change rewraps and drops it", func(t *testing.T) {
		m := armTranscriptSelection(t, modelWithTranscript(t, "hello world"))
		m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
		if m.transcriptSel.active {
			t.Fatal("a rewrap did not drop the transcript selection")
		}
	})
	t.Run("a height-only change keeps it", func(t *testing.T) {
		m := armTranscriptSelection(t, modelWithTranscript(t, "hello world"))
		m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
		if !m.transcriptSel.active {
			t.Fatal("a height-only resize dropped a selection whose lines are unchanged")
		}
	})
}

// TestTranscriptHighlightPersistsWhileStreaming checks the lingering post-copy highlight obeys the
// same rule as a live drag: what was copied stays visibly marked while the reply streams below it.
func TestTranscriptHighlightPersistsWhileStreaming(t *testing.T) {
	m := armTranscriptSelection(t, modelWithTranscript(t, "hello world"))
	m, cmd := stepCmd(t, m, leftRelease(m.viewport.Width(), 0))
	if cmd == nil {
		t.Fatal("precondition: the release did not copy")
	}

	for _, tok := range []string{"one ", "two ", "three"} {
		m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: tok}})
	}
	if !m.transcriptSel.active {
		t.Fatal("the stream dropped the copied highlight")
	}
	if !strings.Contains(m.View().Content, selectionBg) {
		t.Fatal("the copied span is no longer shaded in the View")
	}
}

// TestNotedBeatRepaintKeepsSelection checks the heartbeat's repaint is now harmless to a selection:
// a beat that MOVED something (the upstream going offline) appends its note and re-renders, and a
// selection over the settled lines above it is untouched. The beat fold's repaint guard is economy
// from here on, not what keeps a drag alive.
func TestNotedBeatRepaintKeepsSelection(t *testing.T) {
	m := wireHeartbeat(t, testOpts, &fakeHeartbeat{})
	m.transcript.addUser("hello world", nil)
	m.refreshViewport()
	m = armTranscriptSelection(t, m)

	before := len(noteTexts(m))
	m = foldBeatMsg(t, m, downBeat("dial tcp: connection refused"))
	if len(noteTexts(m)) == before {
		t.Fatal("precondition: the beat noted nothing, so it never repainted")
	}
	if !m.transcriptSel.active {
		t.Fatal("a noted-beat repaint dropped a selection over settled lines")
	}
}

// TestSpanUnchangedTable is the predicate itself: what "the ground did not move" means, line by
// line, independent of any repaint that consults it.
func TestSpanUnchangedTable(t *testing.T) {
	span := func(a, b contentCell) transcriptSel {
		return transcriptSel{active: true, anchor: a, head: b}
	}
	cases := []struct {
		name                string
		sel                 transcriptSel
		oldLines, nextLines []string
		want                bool
	}{
		{
			"an inactive selection has nothing to keep",
			transcriptSel{anchor: contentCell{0, 0}, head: contentCell{1, 2}},
			[]string{"a", "b"}, []string{"a", "b"}, false,
		},
		{
			"an unchanged span is kept while the tail below it grows",
			span(contentCell{0, 0}, contentCell{1, 2}),
			[]string{"a", "b"}, []string{"a", "b", "c"}, true,
		},
		{
			"one changed line inside the span drops it",
			span(contentCell{0, 0}, contentCell{2, 1}),
			[]string{"a", "b", "c"}, []string{"a", "X", "c"}, false,
		},
		{
			"a change outside the span is none of its business",
			span(contentCell{0, 0}, contentCell{1, 1}),
			[]string{"a", "b", "c"}, []string{"a", "b", "X"}, true,
		},
		{
			"a reversed anchor/head normalises to the same rows",
			span(contentCell{2, 1}, contentCell{0, 0}),
			[]string{"a", "b", "c"}, []string{"a", "X", "c"}, false,
		},
		{
			"a span past the incoming lines drops it",
			span(contentCell{0, 0}, contentCell{2, 1}),
			[]string{"a", "b", "c"}, []string{"a", "b"}, false,
		},
		{
			"a span past the outgoing lines drops it",
			span(contentCell{0, 0}, contentCell{2, 1}),
			[]string{"a", "b"}, []string{"a", "b", "c"}, false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.sel.spanUnchanged(c.oldLines, c.nextLines); got != c.want {
				t.Fatalf("spanUnchanged(%q, %q) = %v, want %v", c.oldLines, c.nextLines, got, c.want)
			}
		})
	}
}

// TestPromptAndTranscriptSelectionsAreExclusive checks the region arbitration: starting one
// selection clears the other, so the prompt and transcript selections never coexist.
func TestPromptAndTranscriptSelectionsAreExclusive(t *testing.T) {
	m := newTestModel(t)
	m.transcript.addUser("hello world", nil)
	m.input.SetValue("prompt text")
	m.layout() // sizes the input box and populates m.lines

	// Arm a transcript selection.
	m = step(t, m, leftClick(0, 0))
	m = step(t, m, leftDrag(5, 0))
	if !m.transcriptSel.active {
		t.Fatal("precondition: transcript selection not armed")
	}

	// A click into the prompt arms the prompt selection and clears the transcript one.
	const yInput = 24 - footerHeight - 1
	m = step(t, m, leftClick(2, yInput))
	if m.transcriptSel.active {
		t.Fatal("a prompt click did not clear the transcript selection")
	}
	if !m.sel.active {
		t.Fatal("a prompt click did not arm the prompt selection")
	}

	// And the reverse: a click into the transcript clears the prompt selection.
	m = step(t, m, leftClick(0, 0))
	m = step(t, m, leftDrag(3, 0))
	if m.sel.active {
		t.Fatal("a transcript click did not clear the prompt selection")
	}
	if !m.transcriptSel.active {
		t.Fatal("a transcript click did not arm the transcript selection")
	}
}

// ----------------------------------------------------------------------------
// Selection scope across the state ladder (mouse.go inputEditable; ADR 0025)
// ----------------------------------------------------------------------------
//
// "Select text at any point in time" reaches the two rectangles differently. The transcript is
// selectable in EVERY state: the mouse cases in Update are the one input path that is not gated by
// state, and (since the keep-if-unchanged rule) a repaint no longer takes a selection away, so
// there is always something on screen a drag can copy. The prompt follows EDITABILITY — idle, the
// borrowed ask answer box, and while a worker runs — because a selection there carries a caret with
// it. At an approval decision and after an error the box is inert (a/d/s and Enter-dismiss own the
// keyboard, and the cursor is hidden), so a click in it selects nothing and falls through to the
// same region arbitration as a click on any other non-field cell. The three tests below pin exactly
// that scope so it cannot narrow again by accident.

// TestPromptDragSelectsWhileRunning is the prompt half of "at any point in time": text typed into
// the box while the model works selects and copies there exactly as it does at idle — the drag runs
// while a worker owns the exchange and the staged interjection is still being written.
func TestPromptDragSelectsWhileRunning(t *testing.T) {
	m := modelWithInput(t, "hello world")
	m.state = stateRunning
	const y = 24 - footerHeight - 1

	m = step(t, m, leftClick(2+0, y)) // anchor at column 0
	m = step(t, m, leftDrag(2+5, y))  // head at column 5 → "hello"
	if !m.sel.active || m.sel.anchorOff != 0 || m.sel.headOff != 5 {
		t.Fatalf("prompt selection while running = %+v, want an active span over offsets (0,5)", m.sel)
	}
	if got := selectionText(m.input.Value(), m.sel.anchorOff, m.sel.headOff); got != "hello" {
		t.Fatalf("selected text = %q, want %q — the runes actually typed", got, "hello")
	}

	m, cmd := stepCmd(t, m, leftRelease(2+5, y))
	if cmd == nil {
		t.Fatal("releasing a prompt drag while running should return a copy Cmd, got nil")
	}
	if !strings.Contains(m.flash, "copied 5 chars") {
		t.Fatalf("flash = %q, want it to mention 'copied 5 chars'", m.flash)
	}
	if m.state != stateRunning {
		t.Fatalf("state = %v after a prompt drag, want it left at running", m.state)
	}
}

// TestTranscriptDragCopiesInEveryState is the transcript half: a drag-release over the settled
// conversation copies in all five states — including the running one, where the issue this pin
// answers used to lose the selection to the next streamed token.
func TestTranscriptDragCopiesInEveryState(t *testing.T) {
	states := []struct {
		name  string
		state uiState
	}{
		{"idle", stateIdle},
		{"running", stateRunning},
		{"awaiting approval", stateAwaitingApproval},
		{"awaiting ask", stateAwaitingAsk},
		{"errored", stateErrored},
	}
	for _, s := range states {
		t.Run(s.name, func(t *testing.T) {
			m := modelWithTranscript(t, "hello world")
			m.state = s.state
			w := m.viewport.Width()

			m = step(t, m, leftClick(0, 0)) // the settled user block, pinned to the top row
			m = step(t, m, leftDrag(w, 0))
			if !m.transcriptSel.active {
				t.Fatal("a transcript drag armed no selection")
			}
			got := transcriptSelectionText(m.th.measure, m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
			if want := glyphUser + " hello world"; got != want {
				t.Fatalf("selected text = %q, want %q", got, want)
			}

			m, cmd := stepCmd(t, m, leftRelease(w, 0))
			if cmd == nil {
				t.Fatal("release of a non-empty transcript selection should return a copy Cmd, got nil")
			}
			if !strings.Contains(m.flash, "copied") {
				t.Fatalf("flash = %q, want a copy confirmation", m.flash)
			}
		})
	}
}

// TestPromptClickRefusedAtApprovalAndErrored pins the scope's boundary: where the box is inert the
// mouse leaves it alone — no caret move, no selection armed, nothing copied. The click is not
// swallowed either; it goes on to the region arbitration exactly as a click on any other cell does,
// which is what keeps the transcript copyable in those states (the test above).
func TestPromptClickRefusedAtApprovalAndErrored(t *testing.T) {
	for _, state := range []uiState{stateAwaitingApproval, stateErrored} {
		m := modelWithInput(t, "hello world")
		m.state = state
		const y = 24 - footerHeight - 1
		caret := m.input.Column() // SetValue leaves it after the text

		m = step(t, m, leftClick(2+0, y))
		if m.sel.active {
			t.Fatalf("state %v: a click armed a prompt selection in an inert box: %+v", state, m.sel)
		}
		if got := m.input.Column(); got != caret {
			t.Fatalf("state %v: a click moved the caret to column %d, want it left at %d", state, got, caret)
		}

		m = step(t, m, leftDrag(2+5, y))
		m, cmd := stepCmd(t, m, leftRelease(2+5, y))
		if cmd != nil {
			t.Fatalf("state %v: a drag in an inert box copied something", state)
		}
		if m.flash != "" {
			t.Fatalf("state %v: flash = %q, want empty — nothing was selected", state, m.flash)
		}
	}
}

// TestTranscriptSelectionOnStickyHeaderRow checks the sticky-header row copies WHAT IS DRAWN ON
// IT: a drag over row 0 takes the overlaid prompt, not the reply line the scroll offset hides
// beneath the overlay, and the highlight reaches the composed View (which layers the highlight
// over the sticky-header overlay). Both scroll regimes are covered — parked on the prompt row,
// where the overlay is a visual no-op, and following the tail of a reply taller than the screen,
// where the overlay genuinely covers a different content line (the default for a long reply).
func TestTranscriptSelectionOnStickyHeaderRow(t *testing.T) {
	base := func(t *testing.T) Model {
		t.Helper()
		m := newTestModel(t)
		m.transcript.addUser("HEADERPROMPT", nil)
		for i := 0; i < 40; i++ {
			m.transcript.commitAssistant("reply "+strings.Repeat("x", 5), 0)
		}
		m.refreshViewport()
		return m
	}
	cases := []struct {
		name string
		park func(m *Model)
	}{
		{"parked on the prompt row", func(m *Model) {
			m.detached = true
			m.viewport.SetYOffset(m.userBlocks[len(m.userBlocks)-1].start)
		}},
		{"following the tail", func(m *Model) {
			if m.viewport.YOffset() <= m.userBlocks[len(m.userBlocks)-1].start {
				t.Fatalf("setup: the tail offset %d must sit below the prompt row %d for the overlay to cover anything",
					m.viewport.YOffset(), m.userBlocks[len(m.userBlocks)-1].start)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := base(t)
			c.park(&m)

			w := m.viewport.Width()
			m = step(t, m, leftClick(0, 0))
			m = step(t, m, leftDrag(w, 0))
			got := transcriptSelectionText(m.th.measure, m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
			if !strings.Contains(got, "HEADERPROMPT") {
				t.Fatalf("selecting the sticky-header row copied %q, want it to contain the prompt text", got)
			}
			if !strings.Contains(m.View().Content, selectionBg) {
				t.Fatal("a selection over the sticky-header row did not reach the rendered View")
			}
		})
	}
}

// ----------------------------------------------------------------------------
// One frame-row derivation: View, the mouse, and the overlays (item 7)
// ----------------------------------------------------------------------------

// TestMouseClickOnOverlayRowsArmsNoSelection is the audit's 80×24 repro. The approval popup is
// painted over the BOTTOM rows of the transcript's slot, but the mouse used to bound a click by the
// height layout() stored — the un-shrunk one — so a left-click on the popup's top border armed a
// transcript selection anchored on the reply line hidden underneath it, and the release put text
// nobody could see on the system clipboard over OSC 52. Every screen row an overlay occupies must
// map to no transcript position at all.
func TestMouseClickOnOverlayRowsArmsNoSelection(t *testing.T) {
	m, _ := newApprovalModel(t, domain.ApprovalRequest{Tool: "write_file", Reason: "write the notes file"})
	for i := range 40 { // a transcript deep enough that the covered rows all name real content
		m.transcript.commitAssistant(fmt.Sprintf("reply line %02d", i), 0)
	}
	m.refreshViewport()

	drawn, laidOut := m.transcriptRows(), m.viewport.Height()
	if drawn >= laidOut {
		t.Fatalf("setup: the approval popup took no rows off the transcript (drawn %d, laid out %d)", drawn, laidOut)
	}
	if m.contentLineAt(drawn) >= 0 {
		t.Errorf("the first overlay row still maps to content line %d", m.contentLineAt(drawn))
	}
	for y := drawn; y < laidOut; y++ {
		if line, _, ok := m.pointTranscriptRow(4, y); ok {
			t.Errorf("row %d is painted by the popup, yet it maps to content line %d", y, line)
		}
		if next := step(t, m, leftClick(4, y)); next.transcriptSel.active {
			t.Errorf("a click on popup row %d armed a transcript selection", y)
		}
	}

	// The whole gesture, not just the press: a drag across the popup copies nothing and flashes
	// nothing, because it never anchored anywhere.
	m = step(t, m, leftClick(4, drawn))
	m = step(t, m, leftDrag(40, laidOut-1))
	m, cmd := stepCmd(t, m, leftRelease(40, laidOut-1))
	if cmd != nil {
		t.Error("a drag over the popup returned a Cmd; nothing may reach the clipboard from there")
	}
	if m.flash != "" {
		t.Errorf("flash = %q after a drag over the popup, want no copy confirmation", m.flash)
	}
}

// TestFrameRowBoundaryAgreesWithTheMouseMapping is the seam itself, over a table of overlay
// heights: the row at which View stops drawing the transcript and starts drawing the overlay is
// exactly the row at which the mouse stops finding transcript content. One derivation, so the two
// cannot drift — and the composed frame still fits the terminal.
func TestFrameRowBoundaryAgreesWithTheMouseMapping(t *testing.T) {
	deepTranscript := func(m *Model) {
		for i := range 60 {
			m.transcript.commitAssistant(fmt.Sprintf("reply line %02d", i), 0)
		}
		m.refreshViewport()
	}
	cases := []struct {
		name  string
		build func(t *testing.T) (Model, string) // the model, and the overlay block drawn below the transcript
	}{
		{"approval prompt, one-line reason", func(t *testing.T) (Model, string) {
			m, _ := newApprovalModel(t, domain.ApprovalRequest{Tool: "run", Reason: "go"})
			deepTranscript(&m)
			return m, m.frameOverlays().prompt
		}},
		{"approval prompt, a body that wraps", func(t *testing.T) (Model, string) {
			m, _ := newApprovalModel(t, domain.ApprovalRequest{
				Tool:   "write_file",
				Reason: strings.Repeat("a rather long reason that has to wrap across several lines ", 4),
			})
			deepTranscript(&m)
			return m, m.frameOverlays().prompt
		}},
		{"ask prompt with choices", func(t *testing.T) (Model, string) {
			m := newTestModel(t)
			deepTranscript(&m)
			m = step(t, m, askReqMsg{
				Request: domain.AskRequest{Question: "which one?", Choices: []string{"a", "b", "c", "d", "e"}},
				Reply:   make(chan domain.AskAnswer, 1),
			})
			return m, m.frameOverlays().prompt
		}},
		{"sessions browser", func(t *testing.T) (Model, string) {
			m := newTestModel(t)
			deepTranscript(&m)
			m.sessionBrowser = browserWithSessions(12)
			return m, m.frameOverlays().browser
		}},
		{"no overlay at all", func(t *testing.T) (Model, string) {
			m := newTestModel(t)
			deepTranscript(&m)
			return m, ""
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, overlay := c.build(t)
			drawn := m.transcriptRows()

			// The mouse's own boundary: the last transcript row maps, the first row past it does not.
			if drawn > 0 {
				if _, _, ok := m.pointTranscriptRow(0, drawn-1); !ok {
					t.Errorf("row %d is the last transcript row but the mouse maps nothing there", drawn-1)
				}
			}
			if line, _, ok := m.pointTranscriptRow(0, drawn); ok {
				t.Errorf("row %d is past the transcript but the mouse maps it to content line %d", drawn, line)
			}

			// View's own boundary: the overlay's first line is painted on exactly that row.
			frame := strings.Split(m.View().Content, "\n")
			if overlay != "" {
				want := strings.Split(overlay, "\n")[0]
				if drawn >= len(frame) {
					t.Fatalf("frame has %d rows, too short to hold the boundary row %d", len(frame), drawn)
				}
				if frame[drawn] != want {
					t.Errorf("frame row %d = %q, want the overlay's first line %q",
						drawn, ansiPattern.ReplaceAllString(frame[drawn], ""), ansiPattern.ReplaceAllString(want, ""))
				}
			}
			if len(frame) > m.height {
				t.Errorf("composed frame is %d rows on a %d-row terminal", len(frame), m.height)
			}
		})
	}
}
