package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

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

// TestClickIgnoredWhileRunning checks that mouse clicks do not edit the refused input while a
// worker is in flight.
func TestClickIgnoredWhileRunning(t *testing.T) {
	m := modelWithInput(t, "hello world")
	m.state = stateRunning
	m.input.MoveToEnd()
	wantCol := m.input.Column()

	m = step(t, m, leftClick(2+0, 24-footerHeight-1)) // would move caret to column 0 if honoured
	if m.input.Column() != wantCol {
		t.Fatalf("click moved the caret while running (col %d, want %d)", m.input.Column(), wantCol)
	}
	if m.sel.active {
		t.Fatal("click should not arm a selection while running")
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
	out := shadeCells(line, 2, 5, newTestModel(t).th.selection)
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
// boundary — for any script — using the same runewidth the textarea's cursor math uses. It holds
// only for runs of non-zero-width runes (the prompt content), so the fixtures avoid combining
// marks.
func TestCellToRuneOffsetInvertsWidth(t *testing.T) {
	for _, s := range []string{"hello", "日本語 text", "aあb🙂c", ""} {
		runes := []rune(s)
		acc := 0
		for k := 0; k <= len(runes); k++ {
			if got := cellToRuneOffset(runes, acc); got != k {
				t.Errorf("%q: cellToRuneOffset(., %d cells) = %d, want boundary %d", s, acc, got, k)
			}
			if k < len(runes) {
				acc += runewidth.RuneWidth(runes[k])
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

// TestPasteIgnoredWhileRunning checks a paste is dropped while a worker holds the input, the same
// way keypress edits are refused in that state.
func TestPasteIgnoredWhileRunning(t *testing.T) {
	m := modelWithInput(t, "keep")
	m.state = stateRunning
	m = step(t, m, tea.PasteMsg{Content: "junk"})
	if got := m.input.Value(); got != "keep" {
		t.Fatalf("paste while running must not edit the input, got %q", got)
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
			got := transcriptSelectionText(lines, c.a, c.b)
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
	got := transcriptSelectionText(m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
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

// TestTranscriptSelectionClearsOnStreamToken checks the selection drops when the rendered lines
// regenerate under a streamed token (its content-anchored coords would otherwise index stale lines).
func TestTranscriptSelectionClearsOnStreamToken(t *testing.T) {
	m := modelWithTranscript(t, "hello world")
	m = step(t, m, leftClick(0, 0))
	m = step(t, m, leftDrag(5, 0))
	if !m.transcriptSel.active {
		t.Fatal("precondition: no transcript selection armed")
	}
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "hi"}})
	if m.transcriptSel.active {
		t.Fatal("a stream token did not clear the transcript selection")
	}
}

// TestTranscriptSelectionClearsOnResize checks a window resize drops the selection (the lines
// reflow to the new width, so the stored cells go stale) — the prompt selection clears the same way.
func TestTranscriptSelectionClearsOnResize(t *testing.T) {
	m := modelWithTranscript(t, "hello world")
	m = step(t, m, leftClick(0, 0))
	m = step(t, m, leftDrag(5, 0))
	if !m.transcriptSel.active {
		t.Fatal("precondition: no transcript selection armed")
	}
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.transcriptSel.active {
		t.Fatal("a resize did not clear the transcript selection")
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

// TestTranscriptSelectionOnStickyHeaderRow checks the pinned sticky-header row behaves as a plain
// viewport row for selection: a drag over it copies its rendered text and the highlight reaches
// the composed View (which layers the highlight over the sticky-header overlay).
func TestTranscriptSelectionOnStickyHeaderRow(t *testing.T) {
	m := newTestModel(t)
	m.transcript.addUser("HEADERPROMPT", nil)
	for i := 0; i < 40; i++ {
		m.transcript.commitAssistant("reply "+strings.Repeat("x", 5), 0)
	}
	m.refreshViewport() // the sole user prompt pins to row 0 as the sticky header

	w := m.viewport.Width()
	m = step(t, m, leftClick(0, 0))
	m = step(t, m, leftDrag(w, 0))
	got := transcriptSelectionText(m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
	if !strings.Contains(got, "HEADERPROMPT") {
		t.Fatalf("selecting the sticky-header row copied %q, want it to contain the prompt text", got)
	}
	if !strings.Contains(m.View().Content, selectionBg) {
		t.Fatal("a selection over the sticky-header row did not reach the rendered View")
	}
}
