package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
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
// (border + padding) and the text row sits at
// y = height - bottomRuleHeight - footerHeight - inputBorderRows = 20 — the box's own bottom
// border, the footer's single line and the ▁ hairline are what stand below it.
func TestClickPositionsCaret(t *testing.T) {
	m := modelWithInput(t, "hello world")
	const textRowY = 24 - bottomRuleHeight - footerHeight - inputBorderRows // single content row, bottom-anchored above the footer

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
	// Two content rows, bottom-anchored: the LAST one ("cd") sits at
	// y = height - bottomRuleHeight - footerHeight - inputBorderRows, whatever the box's height.
	const row1Y = 24 - bottomRuleHeight - footerHeight - inputBorderRows

	m = step(t, m, leftClick(2+1, row1Y)) // x0(2) + column 1 → the 'd'
	if m.input.Line() != 1 || m.input.Column() != 1 {
		t.Fatalf("caret at row %d col %d, want row 1 col 1", m.input.Line(), m.input.Column())
	}
	if m.sel.anchorOff != 4 { // a(0) b(1) \n(2) c(3) d(4)
		t.Fatalf("anchorOff = %d, want 4", m.sel.anchorOff)
	}
}

// fillsTheInputWidth returns a run of the given rune long enough to make its display width REACH
// the prompt's wrap width exactly — the geometry bubbles' wrap answers with a PHANTOM trailing
// sub-line, the seat it keeps for a caret past a full line. The width is asked of the widget
// (Width() is the wrap width after the prompt gutter and borders come off) rather than restated,
// so the fill holds whatever the frame around the box costs.
func fillsTheInputWidth(t *testing.T, m Model, r rune) string {
	t.Helper()
	cells := uniseg.StringWidth(string(r))
	if cells < 1 {
		t.Fatalf("fill rune %q measures %d cells", r, cells)
	}
	fill := strings.Repeat(string(r), m.input.Width()/cells)
	if pad := m.input.Width() - uniseg.StringWidth(fill); pad > 0 {
		fill += strings.Repeat("a", pad) // an odd width tops up with a narrow rune
	}
	return fill
}

// TestClickBelowPhantomWrappedLineSeatsCaret is the mouse-path regression for the phantom
// trailing sub-line. bubbles' wrap appends one to a logical line whose content REACHES the width,
// and its CursorDown can never enter it — the step's column guess clamps at len(line)-1 while that
// sub-line starts at len(line) — so a walk of bare CursorDowns stood still on such a line and a
// click on the row BELOW it landed a row short, on the wrong logical line entirely. The Height-aware
// walk crosses the whole logical line instead, and the phantom row is itself clickable: a click
// there seats the caret at that line's END, where CursorEnd puts the keyboard's.
//
// The CJK case is the same geometry with wide runes, which shift the fill point: the line reaches
// the width in half as many runes, so a walk that counted runes rather than cells would miss it.
func TestClickBelowPhantomWrappedLineSeatsCaret(t *testing.T) {
	for _, tc := range []struct {
		name string
		fill rune
	}{
		{"narrow runes", 'a'},
		{"wide runes", '日'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fill := fillsTheInputWidth(t, modelWithInput(t, ""), tc.fill)
			m := modelWithInput(t, fill+"\nsecond")

			// The geometry the test rests on: the first logical line wraps to TWO visual rows (its
			// content plus the phantom), both on screen, unscrolled.
			m.input.MoveToBegin()
			if got := m.input.LineInfo().Height; got != 2 {
				t.Fatalf("first logical line occupies %d visual rows, want 2 (content + phantom)", got)
			}
			x0, y0, _, h := m.inputContentRect()
			if h < 3 || m.input.ScrollYOffset() != 0 {
				t.Fatalf("box shows %d rows at scroll offset %d, want ≥3 rows unscrolled",
					h, m.input.ScrollYOffset())
			}

			// A click on the row BELOW the phantom names the second logical line's first cell.
			below := step(t, m, leftClick(x0, y0+2))
			if below.input.Line() != 1 || below.input.Column() != 0 {
				t.Fatalf("click below the phantom row seated the caret at row %d col %d, want row 1 col 0",
					below.input.Line(), below.input.Column())
			}
			if want := len([]rune(fill)) + 1; below.sel.anchorOff != want {
				t.Fatalf("anchorOff = %d, want %d (the rune after the newline)", below.sel.anchorOff, want)
			}
			// The auto-grow re-clamp runs on that same seat and must not move it.
			below.reseatInput()
			if below.input.Line() != 1 || below.input.Column() != 0 {
				t.Fatalf("reseatInput moved the caret to row %d col %d", below.input.Line(), below.input.Column())
			}

			// A click ON the phantom row seats the caret at the first logical line's end.
			phantom := step(t, m, leftClick(x0, y0+1))
			if phantom.input.Line() != 0 || phantom.input.Column() != len([]rune(fill)) {
				t.Fatalf("click on the phantom row seated the caret at row %d col %d, want row 0 col %d",
					phantom.input.Line(), phantom.input.Column(), len([]rune(fill)))
			}
			phantom.reseatInput()
			if phantom.input.Line() != 0 || phantom.input.Column() != len([]rune(fill)) {
				t.Fatalf("reseatInput moved the caret to row %d col %d", phantom.input.Line(), phantom.input.Column())
			}
		})
	}
}

// TestDragSelectsAndCopies drives press → drag → release and checks the selection span, the
// copy Cmd, and the confirmation flash.
func TestDragSelectsAndCopies(t *testing.T) {
	m := modelWithInput(t, "hello world")
	const y = 24 - bottomRuleHeight - footerHeight - inputBorderRows

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

// recordSystemClipboard substitutes the system-clipboard seam (clipboard.go) with a recorder that
// returns err, restoring the real one when the test ends. The returned channel carries every text
// handed over. The seam exists precisely because the real write shells out to a platform program
// (pbcopy, xclip, clip.exe) that a unit test cannot count on having.
func recordSystemClipboard(t *testing.T, err error) <-chan string {
	t.Helper()
	previous := writeSystemClipboard
	t.Cleanup(func() { writeSystemClipboard = previous })
	wrote := make(chan string, 4)
	writeSystemClipboard = func(text string) error {
		wrote <- text
		return err
	}
	return wrote
}

// fireBatch runs the Cmds inside a tea.Batch the way the runtime does — each on its own goroutine,
// no ordering — and returns WITHOUT waiting for them. Waiting would mean sitting out copyFlash's
// two-second flash tick, which shares the batch with the two clipboard writes; the caller waits on
// the one Cmd it cares about instead.
func fireBatch(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("copy Cmd produced %T, want a tea.BatchMsg of the clipboard writes and the tick", msg)
	}
	for _, c := range batch {
		go c() //nolint:errcheck // the msgs go nowhere in a test that drives the seam directly
	}
}

// TestDragCopyAlsoWritesTheSystemClipboard pins the fallback the ISSUES defect asked for: the copy
// a drag-release produces hands the SAME text to the host's clipboard program, so a terminal that
// silently drops OSC 52 still ends up holding the selection. OSC 52 stays in the batch beside it —
// this asserts the addition, not a replacement.
func TestDragCopyAlsoWritesTheSystemClipboard(t *testing.T) {
	wrote := recordSystemClipboard(t, nil)

	m := modelWithInput(t, "hello world")
	const y = 24 - bottomRuleHeight - footerHeight - inputBorderRows

	m = step(t, m, leftClick(2+0, y))
	m = step(t, m, leftDrag(2+5, y))
	_, cmd := stepCmd(t, m, leftRelease(2+5, y))
	if cmd == nil {
		t.Fatal("release of a non-empty selection should return a copy Cmd, got nil")
	}
	fireBatch(t, cmd)

	select {
	case got := <-wrote:
		if got != "hello" {
			t.Fatalf("system clipboard received %q, want the selection %q", got, "hello")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the copy never reached the system clipboard")
	}
}

// TestSystemClipboardFailureStillConfirmsTheCopy pins the fallback as BEST-EFFORT: on a machine
// with no clipboard program the copy must degrade to exactly the old OSC-52-only behaviour — the
// confirmation flash stands, the error surfaces nowhere, and nothing panics.
func TestSystemClipboardFailureStillConfirmsTheCopy(t *testing.T) {
	wrote := recordSystemClipboard(t, errors.New("no clipboard program on this host"))

	m := modelWithInput(t, "hello world")
	const y = 24 - bottomRuleHeight - footerHeight - inputBorderRows

	m = step(t, m, leftClick(2+0, y))
	m = step(t, m, leftDrag(2+5, y))
	m, cmd := stepCmd(t, m, leftRelease(2+5, y))
	if cmd == nil {
		t.Fatal("release of a non-empty selection should return a copy Cmd, got nil")
	}
	if !strings.Contains(m.flash, "copied 5 chars") {
		t.Fatalf("flash = %q, want a failed system write to leave 'copied 5 chars' standing", m.flash)
	}
	fireBatch(t, cmd)

	select {
	case <-wrote:
	case <-time.After(5 * time.Second):
		t.Fatal("the copy never reached the system clipboard")
	}
	if msg := systemClipboardCmd("hello")(); msg != nil {
		t.Fatalf("a failed system write produced msg %#v, want nil — it must dispatch nothing", msg)
	}
	<-wrote
}

// TestBareClickReleaseDoesNotCopy ensures a click without a drag leaves the caret but copies
// nothing (no flash, no Cmd) and collapses the selection.
func TestBareClickReleaseDoesNotCopy(t *testing.T) {
	m := modelWithInput(t, "hello world")
	const y = 24 - bottomRuleHeight - footerHeight - inputBorderRows

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

// TestClickOnBottomChromeSelectsNothing is the mouse half of the box closing its own frame. The
// box grew a ╰──╯ bottom border row and the footer shed its three rows for one line plus the ▁
// hairline under it, so below the prompt's last text row there are now exactly three chrome rows —
// the box's bottom border, the footer's line, and the hairline — and none of them addresses
// anything: a click on any one positions no caret, arms no prompt selection, and (being past the
// drawn transcript) arms no transcript selection either. The box's TOP border is asserted beside
// them because it is the same rule one row up.
//
// The rows are DERIVED from inputContentRect and the layout constants rather than counted by hand,
// and the run below them is asserted CONTIGUOUS down to the terminal's last row — which is what
// makes this catch a row the frame gains or loses rather than only the rows it has today.
func TestClickOnBottomChromeSelectsNothing(t *testing.T) {
	m := modelWithInput(t, "hello world")
	m.input.MoveToEnd()
	wantCol := m.input.Column()

	_, y0, _, h := m.inputContentRect()
	rows := []struct {
		name string
		y    int
	}{
		{"box top border", y0 - 1},
		{"box bottom border", y0 + h},
		{"footer line", m.height - bottomRuleHeight - footerHeight},
		{"bottom hairline", m.height - bottomRuleHeight},
	}
	for i := 2; i < len(rows); i++ {
		if rows[i].y != rows[i-1].y+1 {
			t.Fatalf("the %s is at row %d and the %s at %d; the bottom chrome must be one contiguous run",
				rows[i].name, rows[i].y, rows[i-1].name, rows[i-1].y)
		}
	}
	if last := rows[len(rows)-1].y; last != m.height-1 {
		t.Fatalf("the bottom chrome ends at row %d, want the terminal's last row %d", last, m.height-1)
	}

	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			live := m
			live.sel = promptSel{active: true, anchorOff: 0, headOff: 5}

			live = step(t, live, leftClick(2+3, r.y))

			if live.sel.active {
				t.Errorf("a click on the %s armed a prompt selection: %+v", r.name, live.sel)
			}
			if live.transcriptSel.active {
				t.Errorf("a click on the %s armed a transcript selection: %+v", r.name, live.transcriptSel)
			}
			if got := live.input.Column(); got != wantCol {
				t.Errorf("a click on the %s moved the caret to column %d, want it left at %d", r.name, got, wantCol)
			}
		})
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

	m = step(t, m, leftClick(2+0, 24-bottomRuleHeight-footerHeight-inputBorderRows)) // column 0 of the single content row
	if got := m.input.Column(); got != 0 {
		t.Fatalf("click did not position the caret while running (col %d, want 0)", got)
	}
	if !m.sel.active || m.sel.anchorOff != 0 || m.sel.headOff != 0 {
		t.Fatalf("a click while running should arm a collapsed selection at offset 0, got %+v", m.sel)
	}
}

// TestKeypressClearsSelection checks the single chokepoint in handleKey drops a live selection.
// It is the rule for every key that MOVES ON from what was selected. The two destructive keys are
// carved out of it and CONSUME the span instead — backspace and del delete the selected text
// (TestSelectionDeleteKeys) — which is a different fate, not an exception to the clear: the
// chokepoint drops the stale coordinates either way.
func TestKeypressClearsSelection(t *testing.T) {
	m := modelWithInput(t, "hello world")
	m.sel = promptSel{active: true, anchorOff: 0, headOff: 5}

	m = step(t, m, tea.KeyPressMsg{Code: 'x'})
	if m.sel.active {
		t.Fatal("a keypress should clear the mouse selection")
	}
}

// dragSelect drives a real click-drag-release across the prompt's first content row, from display
// cell fromCol to toCol — the mouse path a human takes to highlight text. The row and the left
// margin are ASKED of the layout (inputContentRect) rather than restated, so the helper follows the
// box wherever it sits.
func dragSelect(t *testing.T, m Model, fromCol, toCol int) Model {
	t.Helper()
	x0, y0, _, _ := m.inputContentRect()
	m = step(t, m, leftClick(x0+fromCol, y0))
	m = step(t, m, leftDrag(x0+toCol, y0))
	return step(t, m, leftRelease(x0+toCol, y0))
}

// TestSelectionDeleteKeys is ISSUES.md's "backspace/del on selected text should delete it": with a
// highlight standing, both destructive keys take the whole span and seat the caret where the span
// began — whichever direction the drag ran, since a right-to-left drag names the same text.
func TestSelectionDeleteKeys(t *testing.T) {
	cases := []struct {
		name           string
		fromCol, toCol int
		key            tea.KeyPressMsg
	}{
		{"backspace", 0, 5, tea.KeyPressMsg{Code: tea.KeyBackspace}},
		{"delete", 0, 5, tea.KeyPressMsg{Code: tea.KeyDelete}},
		{"backwards drag names the same span", 5, 0, tea.KeyPressMsg{Code: tea.KeyBackspace}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := dragSelect(t, modelWithInput(t, "hello world"), c.fromCol, c.toCol)
			if !m.sel.active || m.sel.anchorOff == m.sel.headOff {
				t.Fatalf("the drag armed no selection to delete: %+v", m.sel)
			}

			m = step(t, m, c.key)

			if got := m.input.Value(); got != " world" {
				t.Fatalf("input = %q, want the selected range gone", got)
			}
			if row, col := m.input.Line(), m.input.Column(); row != 0 || col != 0 {
				t.Errorf("caret at (%d,%d), want it at the span's start (0,0)", row, col)
			}
			if m.sel.active {
				t.Errorf("the selection outlived the text it named: %+v", m.sel)
			}
		})
	}
}

// The cut is by RUNE, not by byte: a span over multi-byte text loses whole characters, never half
// of one. The difference is invisible in ASCII and corrupts the draft in Japanese.
func TestSelectionDeleteCutsRunes(t *testing.T) {
	m := dragSelect(t, modelWithInput(t, "日本語のテキスト"), 0, 4) // 4 cells = the two double-width glyphs

	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})

	if got, want := m.input.Value(), "語のテキスト"; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
}

// The prompt is editable while a worker runs — the human is typing an interjection into it (ADR
// 0025) — so the selection delete lands there exactly as it does at idle.
func TestSelectionDeleteWhileRunning(t *testing.T) {
	m := modelWithInput(t, "hello world")
	m.state = stateRunning

	m = dragSelect(t, m, 0, 5)
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})

	if got := m.input.Value(); got != " world" {
		t.Fatalf("input = %q, want the selected range gone while a worker runs", got)
	}
}

// With nothing highlighted, backspace keeps both of its existing meanings — the carve-out stands in
// front of them and must stay out of their way. A bare click is among the "nothing highlighted"
// cases: it leaves a COLLAPSED span, which is a caret, not a selection.
func TestBackspaceWithoutSelectionIsUnchanged(t *testing.T) {
	t.Run("non-empty box deletes one rune", func(t *testing.T) {
		m := modelWithInput(t, "hello")
		m.input.MoveToEnd()

		m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
		if got := m.input.Value(); got != "hell" {
			t.Fatalf("input = %q, want a single rune deleted", got)
		}
	})
	t.Run("a bare click deletes one rune", func(t *testing.T) {
		m := dragSelect(t, modelWithInput(t, "hello"), 5, 5) // press and release on the same cell

		m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
		if got := m.input.Value(); got != "hell" {
			t.Fatalf("input = %q, want a single rune deleted", got)
		}
	})
	t.Run("empty box pops the queued interjection", func(t *testing.T) {
		m := newTestModel(t)
		m.pendingInterjections = []queuedInterjection{staged(1, "held row")}

		m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
		if got := m.input.Value(); got != "held row" {
			t.Fatalf("input = %q, want the queued row popped back into the box", got)
		}
	})
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

// selectionBg is the truecolor SGR the dark scheme's `selection` role paints as — the marker that
// the selection background actually reached the rendered output. It is DERIVED from the scheme
// rather than written out: the schemes stay under tuning, so retuning `selection` must never fail a
// test (owner call, 2026-08-11), and what the tests below are about is whether that tone arrives at
// all, not which tone it is.
var selectionBg = func() string {
	r, g, b, _ := lipgloss.Color(scheme.Default().Selection).RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}()

// TestViewRendersSelectionHighlight drives a full drag through Update and confirms the
// selection background appears in the whole-screen View — end-to-end, not just the helper.
func TestViewRendersSelectionHighlight(t *testing.T) {
	m := modelWithInput(t, "hello world")
	const y = 24 - bottomRuleHeight - footerHeight - inputBorderRows
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
	const y = 24 - bottomRuleHeight - footerHeight - inputBorderRows

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
	const y = 24 - bottomRuleHeight - footerHeight - inputBorderRows

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
	row := promptRow(t, m)

	m = step(t, m, leftClick(0, row)) // anchor at the prompt row's first cell
	m = step(t, m, leftDrag(w, row))  // drag past the right edge → the whole row
	if !m.transcriptSel.active {
		t.Fatal("a transcript drag did not arm a selection")
	}
	got := transcriptSelectionText(m.th.measure, m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
	if want := glyphUser + " hello world"; got != want {
		t.Fatalf("selected text = %q, want %q (the rendered user block, pad trimmed)", got, want)
	}

	m, cmd := stepCmd(t, m, leftRelease(w, row))
	if cmd == nil {
		t.Fatal("release of a non-empty transcript selection should return a copy Cmd, got nil")
	}
	if !strings.Contains(m.flash, "copied") {
		t.Fatalf("flash = %q, want a copy confirmation", m.flash)
	}
}

// TestTranscriptBareClickCopiesNothing checks a click without a drag copies nothing (no flash, no
// Cmd) and collapses the selection — the same bare-click rule the prompt follows. It holds on a
// block's header too: a motionless click there toggles the block (its own tests below), and
// toggling is not copying — nothing reaches the clipboard and no confirmation is flashed.
func TestTranscriptBareClickCopiesNothing(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T) (Model, int, int) // the model and the screen cell the click lands on
	}{
		{"on an ordinary line", func(t *testing.T) (Model, int, int) {
			return modelWithTranscript(t, "hello world"), 2, 0
		}},
		{"on a block header", func(t *testing.T) (Model, int, int) {
			m := modelWithToolBlock(t, "ok   a\nok   b\nok   c\nPASS")
			return m, 2, screenRow(t, m, markedLine(t, m, targetHeader))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, x, y := c.build(t)

			m = step(t, m, leftClick(x, y))
			m, cmd := stepCmd(t, m, leftRelease(x, y))
			if cmd != nil {
				t.Fatal("a bare transcript click+release should not copy, got a Cmd")
			}
			if m.flash != "" {
				t.Fatalf("flash = %q, want empty after a bare click", m.flash)
			}
			if m.transcriptSel.active {
				t.Fatal("a bare transcript click+release should collapse the selection")
			}
		})
	}
}

// ----------------------------------------------------------------------------
// A motionless click toggles the block under it (mouse.go, layout.md "Collapsed and expanded
// blocks")
// ----------------------------------------------------------------------------

// modelWithToolBlock builds a ready idle model holding one user prompt and one tool block whose
// result is output. A multi-line body is what gives the block something to reveal, and therefore
// what makes every row it paints a click target at all (render.go's target rule). The seeded start-up box is dropped so the block sits high enough to be on screen at any
// scroll position the tests park at.
func modelWithToolBlock(t *testing.T, output string) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser("run the tests", nil)
	m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: output}})
	m.refreshViewport()
	return m
}

// markedLine returns the index of the first rendered line the painter marked with kind, failing
// when the paint carries none — the fixture no longer exercises the surface under test.
func markedLine(t *testing.T, m Model, kind targetKind) int {
	t.Helper()
	for i, target := range m.lineTargets {
		if target.kind == kind {
			return i
		}
	}
	t.Fatalf("no rendered line is marked %v", kind)
	return -1
}

// blockExpanded reports the state of the block the marked line at index belongs to, read off the
// entry the painter's mark names.
func blockExpanded(t *testing.T, m Model, line int) bool {
	t.Helper()
	if line < 0 || line >= len(m.lineTargets) {
		t.Fatalf("line %d is outside the stashed target map (%d lines)", line, len(m.lineTargets))
	}
	return m.transcript.entries[m.lineTargets[line].entry].expanded
}

// clickCell drives a motionless click — press and release in the same cell, which is what the rule
// is written in terms of — and returns the model it left behind.
func clickCell(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	m = step(t, m, leftClick(x, y))
	return step(t, m, leftRelease(x, y))
}

// TestTranscriptClickTogglesTheBlock is the rule itself: a motionless click anywhere on a block that
// hides something toggles it — its header, its target row, its body once the body is on the screen.
// Every row means the one thing now that the `+N more lines` count rides the leader row's outcome
// slot instead of a line of its own (collapsedRemainder).
func TestTranscriptClickTogglesTheBlock(t *testing.T) {
	const output = "ok   a\nok   b\nok   c\nPASS"

	t.Run("a header toggles, and toggles back", func(t *testing.T) {
		m := modelWithToolBlock(t, output)
		header := markedLine(t, m, targetHeader)
		if blockExpanded(t, m, header) {
			t.Fatal("setup: the block is expanded before any click; collapsed is the default")
		}

		m = clickCell(t, m, 2, screenRow(t, m, header))
		if !blockExpanded(t, m, header) {
			t.Fatal("a click on the header did not expand the block")
		}
		if body := strings.Join(m.lines, "\n"); !strings.Contains(body, "PASS") || strings.Contains(body, "more line") {
			t.Fatalf("the expanded paint did not reach the viewport lines:\n%s", body)
		}

		m = clickCell(t, m, 2, screenRow(t, m, header))
		if blockExpanded(t, m, header) {
			t.Fatal("a second click on the header did not collapse the block")
		}
		if body := strings.Join(m.lines, "\n"); !strings.Contains(body, "more line") {
			t.Fatalf("the re-collapsed paint kept no remainder marker:\n%s", body)
		}
	})

	// The leader row is where the remainder count now lives, and it is the block's row like any
	// other: the click that used to land on a marker beneath it lands here and TOGGLES, so the same
	// spot closes the block again (collapsedRemainder).
	t.Run("the leader row carrying the count toggles", func(t *testing.T) {
		m := modelWithToolBlock(t, output)
		header := markedLine(t, m, targetHeader)
		leader := header + 1
		if got := m.lineTargets[leader]; got.kind != targetHeader || got.entry != m.lineTargets[header].entry {
			t.Fatalf("setup: line %d is marked %+v, not the block's own leader row", leader, got)
		}
		if !strings.Contains(strip(m.lines[leader]), "+4 more lines") {
			t.Fatalf("setup: the leader row is %q, without the remainder count in its slot", strip(m.lines[leader]))
		}

		m = clickCell(t, m, 6, screenRow(t, m, leader))
		if !blockExpanded(t, m, header) {
			t.Fatal("a click on the leader row did not expand the block")
		}
		m = clickCell(t, m, 6, screenRow(t, m, leader))
		if blockExpanded(t, m, header) {
			t.Fatal("a second click on the leader row did not collapse the block again")
		}
	})

	// A collapsed block paints no body line at all, so the case is asked of the EXPANDED one: the
	// output is where the pointer already is when a reader has finished with it, and the whole
	// block is the click surface (render.go, renderToolBlock).
	t.Run("a body line closes the block", func(t *testing.T) {
		m := modelWithToolBlock(t, output)
		header := markedLine(t, m, targetHeader)
		m = clickCell(t, m, 2, screenRow(t, m, header))
		if !blockExpanded(t, m, header) {
			t.Fatal("setup: the click on the header did not expand the block")
		}
		body := header + 2 // header, branch line, then the body's first line
		if got := m.lineTargets[body]; got.kind != targetHeader || got.entry != m.lineTargets[header].entry {
			t.Fatalf("setup: line %d is marked %+v, not a body row of the block under test", body, got)
		}
		if !strings.Contains(strip(m.lines[body]), "ok   a") {
			t.Fatalf("setup: line %d is %q, not the body's first line", body, strip(m.lines[body]))
		}

		m = clickCell(t, m, 6, screenRow(t, m, body))
		if blockExpanded(t, m, header) {
			t.Fatal("a click on a body line did not collapse the block")
		}
		if painted := strings.Join(m.lines, "\n"); !strings.Contains(painted, "more line") {
			t.Fatalf("the re-collapsed paint kept no remainder marker:\n%s", painted)
		}
	})

	// The see-less footer is the block's last row and exists for nothing but this click
	// (seeLessFooter, render.go): it is where the pointer of a reader who has just read to the end
	// of the output already is.
	t.Run("the see-less footer closes the block", func(t *testing.T) {
		m := modelWithToolBlock(t, output)
		header := markedLine(t, m, targetHeader)
		m = clickCell(t, m, 2, screenRow(t, m, header))
		if !blockExpanded(t, m, header) {
			t.Fatal("setup: the click on the header did not expand the block")
		}
		rows := memberRows(t, m, m.lineTargets[header].entry)
		footer := rows[len(rows)-1]
		if !strings.Contains(strip(m.lines[footer]), promptSeeLess) {
			t.Fatalf("setup: the block's last marked row is %q, not the see-less footer", strip(m.lines[footer]))
		}

		m = clickCell(t, m, 74, screenRow(t, m, footer))
		if blockExpanded(t, m, header) {
			t.Fatal("a click on the see-less footer did not collapse the block")
		}
		if painted := strings.Join(m.lines, "\n"); strings.Contains(painted, promptSeeLess) {
			t.Fatalf("the collapsed block kept a see-less footer:\n%s", painted)
		}
	})

	// A collapsed block's TARGET row is the same surface from the other side: the row a reader is
	// looking at when they want the rest is the clipped path itself, not the label above it. The
	// row does not MOVE when the click lands, either — a leader row is the same one row open and
	// closed (leaderRow, render.go) — so the pointer is still over the surface that toggles it
	// back. What the row says changes: the open block hides nothing, so its slot gives up the
	// "+N more lines" it was counting (collapsedRemainder) and the target takes the cells back.
	t.Run("a clipped target row toggles the block", func(t *testing.T) {
		m := modelWithClippedToolBlock(t)
		rows := markedRows(t, m, targetHeader)
		if len(rows) != 2 {
			t.Fatalf("the collapsed block marks %d rows, want its header and its one target row:\n%s",
				len(rows), strings.Join(m.lines, "\n"))
		}
		target := rows[1] // the branch row itself, the one carrying the cut
		if !strings.Contains(strip(m.lines[target]), clipTail) {
			t.Fatalf("setup: line %d is %q, not a clipped target row", target, strip(m.lines[target]))
		}
		before := strip(m.lines[target])
		if !strings.Contains(before, "more line") {
			t.Fatalf("setup: the collapsed row is %q, without the count of the body behind it", before)
		}

		m = clickCell(t, m, 6, screenRow(t, m, target))
		if !blockExpanded(t, m, rows[0]) {
			t.Fatal("a click on a clipped target row did not expand the block")
		}
		if painted := strings.Join(m.lines, "\n"); !strings.Contains(painted, "PASS") {
			t.Fatalf("the expanded paint revealed no body:\n%s", painted)
		}
		after := strip(m.lines[target])
		if !strings.HasSuffix(after, glyphExpanded) || strings.Contains(after, "more line") {
			t.Errorf("the row under the pointer is %q; want the same leader row wearing %q and counting nothing",
				after, glyphExpanded)
		}
		if got := m.lineTargets[target]; got.kind != targetHeader || got.entry != m.lineTargets[rows[0]].entry {
			t.Errorf("line %d is marked %+v once the block is open, not the block's own row any more", target, got)
		}

		m = clickCell(t, m, 6, screenRow(t, m, target))
		if blockExpanded(t, m, rows[0]) {
			t.Fatal("a second click on the clipped target row did not collapse the block")
		}
	})
}

// modelWithClippedToolBlock builds a ready idle model whose one tool block carries a command far too
// long for the width, so its branch row spends every cell it has and still cuts the target — the row
// a click has to reach being that cut one rather than the header above it.
//
// The output is a BODY of several lines, because a cut target is no longer a reason to toggle
// anything on its own: a leader row is identical open and closed, so a block with nothing beneath it
// wears no indicator and marks no rows at all (TestClippedTargetAloneIsNoToggleTarget is the paint's
// own side of that rule). The body is what makes this block a target; the cut is what puts the
// target row where this test needs it.
func modelWithClippedToolBlock(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	command := "cd . && " + strings.Repeat("echo one-more-fragment && ", 12) + "true"
	m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"` + command + `"}`)}})
	m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID: "c1", Content: "ok   a\nok   b\nok   c\nPASS"}})
	m.refreshViewport()
	return m
}

// markedRows returns every rendered line the painter marked with kind, in paint order — markedLine's
// plural, for the cases that aim at a row other than the first of a block's surface.
func markedRows(t *testing.T, m Model, kind targetKind) []int {
	t.Helper()
	var rows []int
	for i, target := range m.lineTargets {
		if target.kind == kind {
			rows = append(rows, i)
		}
	}
	if len(rows) == 0 {
		t.Fatalf("no rendered line is marked %v", kind)
	}
	return rows
}

// TestTranscriptDragFromHeaderStillSelects is the arbitration: MOTION decides. A drag that starts
// on a header line is a drag-select like any other — it copies the text it ran over and the block
// it started on keeps its state — because a toggle is a click that never moved.
func TestTranscriptDragFromHeaderStillSelects(t *testing.T) {
	m := modelWithToolBlock(t, "ok   a\nok   b\nok   c\nPASS")
	header := markedLine(t, m, targetHeader)
	row := screenRow(t, m, header)

	m = step(t, m, leftClick(0, row))
	m = step(t, m, leftDrag(m.viewport.Width(), row))
	m, cmd := stepCmd(t, m, leftRelease(m.viewport.Width(), row))
	if cmd == nil {
		t.Fatal("a drag across the header returned no copy Cmd")
	}
	if !strings.Contains(m.flash, "copied") {
		t.Fatalf("flash = %q, want a copy confirmation", m.flash)
	}
	if blockExpanded(t, m, header) {
		t.Fatal("a drag that started on the header toggled the block; motion must win")
	}
}

// TestTranscriptToggleKeepsTheClickedHeaderRow is the anchoring invariant: the line under the
// cursor never moves. The body grows and shrinks BELOW the header, so the header must be painted on
// the same screen row after the toggle as before it — in both directions, whether the view was
// following the tail or parked where the human scrolled it.
func TestTranscriptToggleKeepsTheClickedHeaderRow(t *testing.T) {
	const output = "ok   a\nok   b\nok   c\nPASS"
	cases := []struct {
		name  string
		build func(t *testing.T) Model
	}{
		{"following the tail", func(t *testing.T) Model {
			// Deep enough that the tail is a real scroll position: this is the case the anchoring
			// exists for, since refreshViewport's attached path ends at GotoBottom and would slide
			// the header up by every line the expansion added.
			m := newTestModel(t)
			m.transcript.reset()
			m.transcript.addUser("run the tests", nil)
			for i := range 20 {
				m.transcript.commitAssistant(fmt.Sprintf("earlier line %02d", i), runRef{})
			}
			m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
				ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
			m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: output}})
			for i := range 5 { // the block stays on screen at the tail, with room below it
				m.transcript.commitAssistant(fmt.Sprintf("later line %02d", i), runRef{})
			}
			m.refreshViewport()
			if m.detached || m.viewport.YOffset() == 0 {
				t.Fatalf("setup: want the view following the tail at a real offset, got detached=%v offset=%d",
					m.detached, m.viewport.YOffset())
			}
			return m
		}},
		{"scrolled up, detached", func(t *testing.T) Model {
			m := newTestModel(t)
			m.transcript.reset()
			m.transcript.addUser("run the tests", nil)
			for i := range 20 { // scrollback above the block, so it can be scrolled up TO
				m.transcript.commitAssistant(fmt.Sprintf("earlier line %02d", i), runRef{})
			}
			m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
				ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
			m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: output}})
			for i := range 40 { // depth below it, so the parked offset has somewhere to hold
				m.transcript.commitAssistant(fmt.Sprintf("later line %02d", i), runRef{})
			}
			m.refreshViewport()
			m.detached = true
			m.viewport.SetYOffset(markedLine(t, m, targetHeader) - 5) // park the header five rows down
			if m.viewport.AtBottom() {
				t.Fatal("setup: the parked view is still at the tail; the detached case tests nothing")
			}
			return m
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.build(t)
			header := markedLine(t, m, targetHeader)
			row := screenRow(t, m, header)

			m = clickCell(t, m, 2, row)
			if !blockExpanded(t, m, header) {
				t.Fatal("the click did not expand the block")
			}
			if got := screenRow(t, m, markedLine(t, m, targetHeader)); got != row {
				t.Errorf("expanding moved the clicked header from screen row %d to %d", row, got)
			}

			m = clickCell(t, m, 2, row)
			if blockExpanded(t, m, header) {
				t.Fatal("the second click did not collapse the block")
			}
			if got := screenRow(t, m, markedLine(t, m, targetHeader)); got != row {
				t.Errorf("collapsing moved the clicked header from screen row %d to %d", row, got)
			}
		})
	}
}

// TestTranscriptBodyClickKeepsTheAnchorRow is that same invariant asked of the rows the whole-block
// surface added: a reader who closes a block from the bottom of its output must not have the view
// yanked out from under them. The clicked line goes back on the screen row the pointer is resting on
// (refreshViewportAnchored), and because a block shrinks BELOW its header, everything above the
// pointer holds still with it — so the header is on the row it was on before the click.
func TestTranscriptBodyClickKeepsTheAnchorRow(t *testing.T) {
	m := newTestModel(t)
	m.transcript.reset()
	m.transcript.addUser("run the tests", nil)
	for i := range 20 { // scrollback above the block, so the view is parked at a real offset
		m.transcript.commitAssistant(fmt.Sprintf("earlier line %02d", i), runRef{})
	}
	m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID: "c1", Content: "ok   a\nok   b\nok   c\nPASS"}})
	for i := range 40 { // depth below it, so a collapse cannot run the parked offset off the end
		m.transcript.commitAssistant(fmt.Sprintf("later line %02d", i), runRef{})
	}
	m.refreshViewport()
	m.detached = true
	m.viewport.SetYOffset(markedLine(t, m, targetHeader) - 5) // park the header five rows down

	header := markedLine(t, m, targetHeader)
	headerRow := screenRow(t, m, header)
	m = clickCell(t, m, 2, headerRow)
	if !blockExpanded(t, m, header) {
		t.Fatal("setup: the click on the header did not expand the block")
	}
	body := header + 5 // header, branch line, then the body's four rows: PASS is the last
	if got := strip(m.lines[body]); !strings.Contains(got, "PASS") {
		t.Fatalf("setup: line %d is %q, not the body's last row", body, got)
	}

	m = clickCell(t, m, 6, screenRow(t, m, body))
	if blockExpanded(t, m, header) {
		t.Fatal("a click on the body's last row did not collapse the block")
	}
	if got := screenRow(t, m, markedLine(t, m, targetHeader)); got != headerRow {
		t.Errorf("collapsing from a body row moved the header from screen row %d to %d", headerRow, got)
	}
}

// TestTranscriptDragAcrossBodyRowsStillSelects is the arbitration read over the surface the
// whole-block rule widened: the body toggles now, and MOTION still decides. A drag down an expanded
// block's output copies the rows it ran over and leaves the block open, exactly as a drag across the
// header always has (TestTranscriptDragFromHeaderStillSelects) — a toggle is a click that never
// moved, whichever row it started on.
func TestTranscriptDragAcrossBodyRowsStillSelects(t *testing.T) {
	m := modelWithToolBlock(t, "ok   a\nok   b\nok   c\nPASS")
	header := markedLine(t, m, targetHeader)
	m = clickCell(t, m, 2, screenRow(t, m, header))
	if !blockExpanded(t, m, header) {
		t.Fatal("setup: the click on the header did not expand the block")
	}
	first, last := screenRow(t, m, header+2), screenRow(t, m, header+5) // the body's four rows

	m = step(t, m, leftClick(0, first))
	m = step(t, m, leftDrag(m.viewport.Width(), last))
	m, cmd := stepCmd(t, m, leftRelease(m.viewport.Width(), last))
	if cmd == nil {
		t.Fatal("a drag down the body returned no copy Cmd")
	}
	if !strings.Contains(m.flash, "copied") {
		t.Fatalf("flash = %q, want a copy confirmation", m.flash)
	}
	if !blockExpanded(t, m, header) {
		t.Fatal("a drag across the body toggled the block; motion must win")
	}
}

// streamOneScreen folds enough streamed tokens for the tail to outgrow the viewport, which is what
// drives refreshViewport's attached path into GotoBottom and slides the content under a motionless
// pointer. It returns the model the stream left behind.
func streamOneScreen(t *testing.T, m Model) Model {
	t.Helper()
	for range m.viewport.Height() + 4 {
		m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "a streamed line of reply\n"}})
	}
	return m
}

// expandedFlags snapshots every entry's expanded state, so a test can assert that exactly one block
// flipped and every other one was left alone.
func expandedFlags(m Model) []bool {
	flags := make([]bool, len(m.transcript.entries))
	for i, e := range m.transcript.entries {
		flags[i] = e.expanded
	}
	return flags
}

// modelWithTwoToolBlocks builds a ready idle model holding one user prompt and two finished tool
// blocks, so a test can name a block the click did NOT land on. The start-up box is dropped for the
// same reason modelWithToolBlock drops it: the blocks then sit high enough to be aimed at.
//
// An approval note stands between the two calls, and it is what keeps them TWO blocks: consecutive
// same-label calls fold into one group however much body they carry (groupable, render.go), and a
// group has no header to click. Anything that is not a tool call ends a run, so the note both
// separates them and is the least eventful thing that can (TestRenderGroupBreakers).
func modelWithTwoToolBlocks(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser("run the tests", nil)
	for i, output := range []string{"ok   a\nok   b\nok   c\nPASS", "ok   d\nok   e\nok   f\nPASS"} {
		if i > 0 {
			m.transcript.apply(domain.ApprovalEvent{
				Request: domain.ApprovalRequest{Tool: "terminal"}, Decision: domain.ApprovalAllow})
		}
		id := fmt.Sprintf("c%d", i+1)
		m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
			ID: id, Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
		m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: id, Content: output}})
	}
	m.refreshViewport()
	return m
}

// TestTranscriptClickTogglesWhileStreaming is the streaming miss's regression test. refreshViewport
// ends every streamed event at GotoBottom, so in the time a human takes to let the button up the
// content has scrolled out from under a motionless pointer and the release point names a different
// line — almost always no target at all, which is why the toggle silently missed. Resolving from
// the press anchor (content coordinates, which do not move when the view does) is what makes the
// click land: same press, same intent, same answer at any stream rate.
func TestTranscriptClickTogglesWhileStreaming(t *testing.T) {
	m := modelWithToolBlock(t, "ok   a\nok   b\nok   c\nPASS")
	header := markedLine(t, m, targetHeader)
	entry := m.lineTargets[header].entry
	row := screenRow(t, m, header)
	if m.transcript.entries[entry].expanded {
		t.Fatal("setup: the block is expanded before any click; collapsed is the default")
	}

	m = step(t, m, leftClick(2, row))
	m = streamOneScreen(t, m)
	if !m.transcriptSel.active {
		t.Fatal("setup: the press anchor did not survive the stream, so this case tests nothing")
	}
	if line, _, ok := m.pointTranscriptRow(2, row); ok && line == header {
		t.Fatalf("setup: screen row %d still names the pressed header line %d — the stream never scrolled",
			row, header)
	}

	m = step(t, m, leftRelease(2, row)) // the same screen cell, different content beneath it
	if !m.transcript.entries[entry].expanded {
		t.Fatal("a click whose release outlived a scroll did not toggle the pressed block")
	}
}

// TestTranscriptStreamingClickTogglesOnlyThePressedBlock is that rule's other half: the release
// point names some OTHER block's rows once the stream has scrolled, and none of them may flip. The
// press decides, alone.
func TestTranscriptStreamingClickTogglesOnlyThePressedBlock(t *testing.T) {
	m := modelWithTwoToolBlocks(t)
	header := markedLine(t, m, targetHeader)
	pressed := m.lineTargets[header].entry
	row := screenRow(t, m, header)
	before := expandedFlags(m)

	m = step(t, m, leftClick(2, row))
	m = streamOneScreen(t, m)
	if line, _, ok := m.pointTranscriptRow(2, row); ok && line == header {
		t.Fatalf("setup: screen row %d still names the pressed header line %d — the stream never scrolled",
			row, header)
	}

	m = step(t, m, leftRelease(2, row))
	for i, was := range before {
		want := was
		if i == pressed {
			want = !was // the pressed block, and only it, changes state
		}
		if got := m.transcript.entries[i].expanded; got != want {
			t.Errorf("entry %d expanded = %v, want %v (only the pressed entry %d may flip)",
				i, got, want, pressed)
		}
	}
}

// modelWithToolGroup builds a ready idle model holding one user prompt and three consecutive
// Terminal calls,
// each with output — one grouped block whose members every one has a body and so a state of its
// own. The start-up box is dropped so the block sits high enough to be aimed at.
func modelWithToolGroup(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser("check the build", nil)
	for i, c := range [][2]string{
		{"go build ./...", "ok\nbuilt"},
		{"go vet ./...", "clean\nno findings\ndone"},
		{"go test ./...", "ok\nPASS"},
	} {
		id := fmt.Sprintf("c%d", i+1)
		m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: id, Tool: "terminal",
			Arguments: []byte(`{"command":"` + c[0] + `"}`)}})
		m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: id, Content: c[1]}})
	}
	m.refreshViewport()
	return m
}

// memberRows returns the content lines the group's member'th call is painted on, read off the marks
// the painter made — the same accounting the mouse resolves against, so a test aims where a human's
// pointer would land rather than at a hand-counted offset.
func memberRows(t *testing.T, m Model, member int) []int {
	t.Helper()
	var rows []int
	for i, target := range m.lineTargets {
		if target.kind == targetHeader && target.entry == member {
			rows = append(rows, i)
		}
	}
	if len(rows) == 0 {
		t.Fatalf("no rendered row is marked for member %d:\n%s", member, strings.Join(m.lines, "\n"))
	}
	return rows
}

// TestGroupMemberClickTogglesOnlyThatMember is per-member expansion, whole: a group's members each
// own a state, so a click opens the one under the pointer and leaves its siblings — and the group
// itself, which has no state at all — exactly as they were. Every row of the open member closes it
// again, the see-less row it grew included, because the whole member is its own click surface.
func TestGroupMemberClickTogglesOnlyThatMember(t *testing.T) {
	// entries[0] is the prompt, so the run's three calls are entries 1..3 and the sketch's "middle
	// one expanded" (docs/layout/tool-layout.md) is entry 2.
	const groupHead, middle = 1, 2

	open := func(t *testing.T, m Model) Model {
		t.Helper()
		row := memberRows(t, m, middle)[0]
		m = clickCell(t, m, 4, screenRow(t, m, row))
		if !m.transcript.entries[middle].expanded {
			t.Fatal("a click on the middle member's row did not open it")
		}
		for _, sibling := range []int{groupHead, groupHead + 2} {
			if m.transcript.entries[sibling].expanded {
				t.Fatalf("opening entry %d opened entry %d as well", middle, sibling)
			}
		}
		return m
	}

	t.Run("a click opens the member it landed on, alone", func(t *testing.T) {
		m := modelWithToolGroup(t)
		before := strings.Join(m.lines, "\n")
		m = open(t, m)
		if body := strings.Join(m.lines, "\n"); !strings.Contains(body, "no findings") {
			t.Fatalf("the open member's body never reached the viewport:\n%s", body)
		}
		if strings.Contains(strings.Join(m.lines, "\n"), "built") {
			t.Fatalf("a sibling's body came with it:\n%s", strings.Join(m.lines, "\n"))
		}
		if len(m.lines) <= len(strings.Split(before, "\n")) {
			t.Fatal("the group did not grow; the member painted no extra row")
		}
	})

	// Every row of the open member — its first row, its body, and the see-less row closing it —
	// is the same click surface, so the human closes it wherever the pointer happens to be.
	t.Run("any row of the open member closes it", func(t *testing.T) {
		rows := memberRows(t, open(t, modelWithToolGroup(t)), middle)
		if len(rows) < 3 {
			t.Fatalf("the open member paints %d rows; the case needs a first row, a body and a see-less row", len(rows))
		}
		last := rows[len(rows)-1]
		for _, row := range []int{rows[0], rows[len(rows)/2], last} {
			m := open(t, modelWithToolGroup(t)) // a fresh open member per row: each closes from the same state
			if row == last && !strings.Contains(strip(m.lines[row]), promptSeeLess) {
				t.Fatalf("setup: the member's last row is %q, not the see-less row", strip(m.lines[row]))
			}
			m = clickCell(t, m, 4, screenRow(t, m, row))
			if m.transcript.entries[middle].expanded {
				t.Errorf("a click on the open member's row %d did not close it", row)
			}
		}
	})

	t.Run("the siblings and the header stay put", func(t *testing.T) {
		m := modelWithToolGroup(t)
		before := expandedFlags(m)
		m = open(t, m)
		for i, was := range expandedFlags(m) {
			want := before[i]
			if i == middle {
				want = !before[i]
			}
			if was != want {
				t.Errorf("entry %d expanded = %v, want %v (only the clicked member may flip)", i, was, want)
			}
		}
		// The header itself is no click target: a click there keeps its selection meaning.
		header := memberRows(t, m, groupHead)[0] - 1
		if m.lineTargets[header].kind != targetNone {
			t.Fatalf("setup: line %d is marked %v, not the inert group header this case needs",
				header, m.lineTargets[header].kind)
		}
		painted := strings.Join(m.lines, "\n")
		m = clickCell(t, m, 2, screenRow(t, m, header))
		if got := strings.Join(m.lines, "\n"); got != painted {
			t.Errorf("a click on the group header repainted the transcript:\n--- got ---\n%s\n--- want ---\n%s",
				got, painted)
		}
	})
}

// modelWithLiveToolBlock builds a RUNNING model whose transcript holds a sub-agent run still doing
// its work: the head call has no report yet and a child call is open inside its span, so the block
// is LIVE and its star alternates with the spinner phase — every tick rewrites the very header line
// a press rests on. A run is the live block that is also CLICKABLE: it elides its span
// (blockState.elides), which marks the header a toggle target however short the head's own report
// is, where a plain call still in flight has no body to hide and so is no target at all.
func modelWithLiveToolBlock(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.input.SetValue("survey the tests")
	m = step(t, m, keyEnter()) // submit: the state the spinner chain ticks in
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v after a submit, want running", m.state)
	}
	m.transcript.reset() // drop the seeded start-up box and the send: the block sits high enough to aim at
	m.transcript.addUser("survey the tests", nil)
	m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "s1", Tool: subAgentToolName, Arguments: []byte(`{"prompt":"survey the tests"}`)}})
	m.transcript.apply(domain.ToolCallEvent{ // inside the run, and still waiting for its result
		EventBase: domain.EventBase{Depth: 1},
		Call:      domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	m.refreshViewport()
	return m
}

// TestTranscriptClickTogglesALiveBlockAcrossTheBlink is the live-header regression test. A block
// still holding an open call paints ✦ or a bare cell from the spinner's phase, so the tick rewrites
// its header every half second — and the keep-if-unchanged rule used to zero the press anchor on
// exactly that line, which is the answer the release needs. Collapsed spans are now exempt
// (spanUnchanged), so the press outlives the blink and the toggle lands on a running tool like any
// other.
func TestTranscriptClickTogglesALiveBlockAcrossTheBlink(t *testing.T) {
	m := modelWithLiveToolBlock(t)
	header := markedLine(t, m, targetHeader)
	entry := m.lineTargets[header].entry
	row := screenRow(t, m, header)
	if m.transcript.entries[entry].expanded {
		t.Fatal("setup: the block is expanded before any click; collapsed is the default")
	}

	m = step(t, m, leftClick(2, row))
	painted := m.lines[header]
	m.spin.frame = m.spin.framesPerBlinkHalf() - 1  // …so the next tick is the one that crosses the phase
	m = step(t, m, spinnerTickMsg{gen: m.spin.gen}) // the star flips: the pressed line is rewritten
	if m.lines[header] == painted {
		t.Fatal("setup: the tick left the header line alone, so this case tests nothing")
	}
	if !m.transcriptSel.active {
		t.Fatal("the blinking star zeroed the press before the button came up")
	}

	m = step(t, m, leftRelease(2, row))
	if !m.transcript.entries[entry].expanded {
		t.Fatal("a click held across a star blink did not toggle the live block")
	}
}

// hugePromptBody is a send whose body wraps well past promptCollapsedRows at any test width — the
// prompt that collapses, and therefore the one whose every row is a click target (render.go).
const hugePromptBody = "alpha\nbravo\ncharlie\ndelta\necho\nfoxtrot"

// modelWithHugePrompt builds a ready idle model holding one over-threshold prompt and a short reply
// beneath it. The seeded start-up box is dropped so the block sits high enough to be on screen at
// any parked offset.
func modelWithHugePrompt(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser(hugePromptBody, nil)
	m.transcript.commitAssistant("a short reply", runRef{})
	m.refreshViewport()
	return m
}

// promptBlockLine returns the rendered line at offset rows into the latest user block, checking it
// is inside the block as painted — a test aiming at a row of the block must fail loudly if the
// block stopped painting that many rather than silently clicking somewhere else.
func promptBlockLine(t *testing.T, m Model, offset int) int {
	t.Helper()
	if len(m.userBlocks) == 0 {
		t.Fatal("the transcript holds no user block to aim at")
	}
	b := m.userBlocks[len(m.userBlocks)-1]
	if offset < 0 || offset >= b.count {
		t.Fatalf("offset %d is outside the prompt block's %d painted rows", offset, b.count)
	}
	return b.start + offset
}

// TestTranscriptClickTogglesThePromptBlock is the prompt's own toggle rule (layout.md, "Collapsed
// and expanded blocks"): the WHOLE block is the click surface, so a motionless click on any of its
// rows — the first, the truncated row carrying the see-more marker — opens the prompt, and a second
// click on that same row closes it again.
func TestTranscriptClickTogglesThePromptBlock(t *testing.T) {
	rows := map[string]int{
		"the block's first row": 0,
		"the marker row":        promptCollapsedRows - 1,
	}
	for name, offset := range rows {
		t.Run(name, func(t *testing.T) {
			m := modelWithHugePrompt(t)
			line := promptBlockLine(t, m, offset)
			if blockExpanded(t, m, line) {
				t.Fatal("setup: the prompt is expanded before any click; collapsed is the default")
			}
			if body := strings.Join(m.lines, "\n"); !strings.Contains(body, "see more") {
				t.Fatalf("setup: the prompt did not collapse, so there is nothing to toggle:\n%s", strip(body))
			}

			m = clickCell(t, m, 2, screenRow(t, m, line))
			if !blockExpanded(t, m, line) {
				t.Fatal("a click on the prompt did not expand it")
			}
			if body := strip(strings.Join(m.lines, "\n")); !strings.Contains(body, "foxtrot") || !strings.Contains(body, "see less") {
				t.Fatalf("the expanded paint did not reach the viewport lines:\n%s", body)
			}

			m = clickCell(t, m, 2, screenRow(t, m, line))
			if blockExpanded(t, m, line) {
				t.Fatal("a second click on the prompt did not collapse it")
			}
			if body := strip(strings.Join(m.lines, "\n")); strings.Contains(body, "foxtrot") {
				t.Fatalf("the re-collapsed paint still shows the hidden rows:\n%s", body)
			}
		})
	}
}

// TestTranscriptDragFromAPromptRowStillSelects is the arbitration on the new surface: motion decides
// there exactly as it does on a tool header. A drag across a prompt row copies the text it ran over
// and leaves the block's state alone — a toggle is a click that never moved.
func TestTranscriptDragFromAPromptRowStillSelects(t *testing.T) {
	m := modelWithHugePrompt(t)
	line := promptBlockLine(t, m, 0)
	row := screenRow(t, m, line)

	m = step(t, m, leftClick(0, row))
	m = step(t, m, leftDrag(m.viewport.Width(), row))
	m, cmd := stepCmd(t, m, leftRelease(m.viewport.Width(), row))
	if cmd == nil {
		t.Fatal("a drag across the prompt returned no copy Cmd")
	}
	if !strings.Contains(m.flash, "copied") {
		t.Fatalf("flash = %q, want a copy confirmation", m.flash)
	}
	if blockExpanded(t, m, line) {
		t.Fatal("a drag that started on the prompt toggled it; motion must win")
	}
}

// TestTranscriptSelectionWinsOverTheSkillAccent is the two overlays' composition order on the
// transcript — the prompt box's own rule (TestSelectionWinsOverTheAccent) read on the other
// surface. The block paints its skill accent as it renders; the drag-selection shades the composed
// frame afterwards, so a selected token reads as SELECTED rather than keeping its violet.
func TestTranscriptSelectionWinsOverTheSkillAccent(t *testing.T) {
	const text = "/review this diff"
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser(text, []skillSpan{{start: 0, end: len("/review")}})
	m.refreshViewport()

	line := promptBlockLine(t, m, 0)
	row := screenRow(t, m, line)
	m = step(t, m, leftClick(0, row))
	m = step(t, m, leftDrag(m.th.measure.Width(glyphUser+" "+text), row))

	frame := m.View().Content
	if run := shadedRun(t, frame); !strings.Contains(run, "/review") {
		t.Errorf("the selection does not cover the accented token: %q", run)
	}
	if strings.Contains(frame, m.th.skillAccent.Render("/review")) {
		t.Error("the accent survived under a selection covering the whole token")
	}
}

// TestTranscriptPromptToggleKeepsTheClickedRow is the anchoring invariant on the prompt: the body
// grows and shrinks BELOW the clicked row, so that row is painted on the same screen row after the
// toggle as before it — in both directions, with the view following a tail deep enough that the
// attached repaint would otherwise slide the block by every line the expansion added.
func TestTranscriptPromptToggleKeepsTheClickedRow(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	for i := range 20 { // scrollback above the prompt, so the tail is a real scroll position
		m.transcript.commitAssistant(fmt.Sprintf("earlier line %02d", i), runRef{})
	}
	m.transcript.addUser(hugePromptBody, nil)
	for i := range 5 { // the block stays on screen at the tail, with room below it
		m.transcript.commitAssistant(fmt.Sprintf("later line %02d", i), runRef{})
	}
	m.refreshViewport()
	if m.detached || m.viewport.YOffset() == 0 {
		t.Fatalf("setup: want the view following the tail at a real offset, got detached=%v offset=%d",
			m.detached, m.viewport.YOffset())
	}

	line := promptBlockLine(t, m, promptCollapsedRows-1) // the marker row: the last row above the growth
	row := screenRow(t, m, line)

	m = clickCell(t, m, 2, row)
	if !blockExpanded(t, m, line) {
		t.Fatal("the click did not expand the prompt")
	}
	if got := screenRow(t, m, line); got != row {
		t.Errorf("expanding moved the clicked row from screen row %d to %d", row, got)
	}

	m = clickCell(t, m, 2, row)
	if blockExpanded(t, m, line) {
		t.Fatal("the second click did not collapse the prompt")
	}
	if got := screenRow(t, m, line); got != row {
		t.Errorf("collapsing moved the clicked row from screen row %d to %d", row, got)
	}
}

// TestTranscriptSelectionSurvivesWheelScroll checks the content-anchored selection is untouched by
// a mid-drag wheel scroll: the anchor names a content line, not a screen row, so scrolling moves
// what is on screen without moving (or clearing) the selection.
func TestTranscriptSelectionSurvivesWheelScroll(t *testing.T) {
	m := newTestModel(t)
	m.transcript.addUser("top prompt", nil)
	for i := 0; i < 40; i++ {
		m.transcript.commitAssistant("reply "+strings.Repeat("x", 5), runRef{})
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

// promptRow is the viewport row the latest user prompt's first line is drawn on. A short
// transcript is no longer scrolled so its prompt tops the screen (the submit-time pad is gone),
// so a test aiming at the prompt block asks the paint where the block actually landed.
func promptRow(t *testing.T, m Model) int {
	t.Helper()
	if len(m.userBlocks) == 0 {
		t.Fatal("the transcript holds no user block to aim at")
	}
	return screenRow(t, m, m.userBlocks[len(m.userBlocks)-1].start)
}

// armTranscriptSelection drags across the whole of one viewport row and returns the model with
// that selection live. The row is the caller's to name — the pad that used to put the latest
// prompt on row 0 is gone, so which content a row holds is a fact about the paint, not a constant.
func armTranscriptSelection(t *testing.T, m Model, row int) Model {
	t.Helper()
	m = step(t, m, leftClick(0, row))
	m = step(t, m, leftDrag(m.viewport.Width(), row))
	if !m.transcriptSel.active {
		t.Fatal("precondition: no transcript selection armed")
	}
	return m
}

// armPromptSelection arms that selection across the latest user prompt's own row.
func armPromptSelection(t *testing.T, m Model) Model {
	t.Helper()
	return armTranscriptSelection(t, m, promptRow(t, m))
}

// TestTranscriptSelectionSurvivesStreamAppend is the keep-if-unchanged rule's headline case: a
// selection over settled text lives through the repaint every streamed token causes, stays
// highlighted while the reply grows beneath it, and copies exactly the text still on screen. It
// replaces TestTranscriptSelectionClearsOnStreamToken, which pinned the old clear-on-every-repaint
// behaviour a drag could not survive.
func TestTranscriptSelectionSurvivesStreamAppend(t *testing.T) {
	m := modelWithTranscript(t, "hello world")
	m = armTranscriptSelection(t, m, promptRow(t, m))

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
	row := promptRow(t, m)

	m = step(t, m, leftClick(0, row))
	m = step(t, m, leftDrag(5, row))
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "tokens landing mid-drag"}})
	if !m.transcriptSel.active {
		t.Fatal("a repaint between two drag motions killed the drag")
	}

	m = step(t, m, leftDrag(w, row)) // the drag carries on to the end of the row
	m, cmd := stepCmd(t, m, leftRelease(w, row))
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
		m := armPromptSelection(t, modelWithTranscript(t, "hello world"))
		m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
		if m.transcriptSel.active {
			t.Fatal("a rewrap did not drop the transcript selection")
		}
	})
	t.Run("a height-only change keeps it", func(t *testing.T) {
		m := armPromptSelection(t, modelWithTranscript(t, "hello world"))
		m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
		if !m.transcriptSel.active {
			t.Fatal("a height-only resize dropped a selection whose lines are unchanged")
		}
	})
}

// TestTranscriptHighlightPersistsWhileStreaming checks the lingering post-copy highlight obeys the
// same rule as a live drag: what was copied stays visibly marked while the reply streams below it.
func TestTranscriptHighlightPersistsWhileStreaming(t *testing.T) {
	m := modelWithTranscript(t, "hello world")
	row := promptRow(t, m)
	m = armTranscriptSelection(t, m, row)
	m, cmd := stepCmd(t, m, leftRelease(m.viewport.Width(), row))
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
	m = armTranscriptSelection(t, m, promptRow(t, m))

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
		{
			"a collapsed span is a click in progress and outlives any change",
			span(contentCell{1, 2}, contentCell{1, 2}),
			[]string{"a", "b", "c"}, []string{"a", "X", "c"}, true,
		},
		{
			"a dragged span over those same changed lines still drops",
			span(contentCell{1, 0}, contentCell{1, 2}),
			[]string{"a", "b", "c"}, []string{"a", "X", "c"}, false,
		},
		{
			"a collapsed span past the incoming lines is kept too — the release bounds-checks it",
			span(contentCell{2, 0}, contentCell{2, 0}),
			[]string{"a", "b", "c"}, []string{"a", "b"}, true,
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
	const yInput = 24 - bottomRuleHeight - footerHeight - inputBorderRows
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
	const y = 24 - bottomRuleHeight - footerHeight - inputBorderRows

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
			row := promptRow(t, m)

			m = step(t, m, leftClick(0, row)) // the settled user block, wherever it landed
			m = step(t, m, leftDrag(w, row))
			if !m.transcriptSel.active {
				t.Fatal("a transcript drag armed no selection")
			}
			got := transcriptSelectionText(m.th.measure, m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
			if want := glyphUser + " hello world"; got != want {
				t.Fatalf("selected text = %q, want %q", got, want)
			}

			m, cmd := stepCmd(t, m, leftRelease(w, row))
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
		const y = 24 - bottomRuleHeight - footerHeight - inputBorderRows
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
			m.transcript.commitAssistant("reply "+strings.Repeat("x", 5), runRef{})
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
		m.transcript.commitAssistant(fmt.Sprintf("reply line %02d", i), runRef{})
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
// heights: the row at which View stops drawing the transcript is exactly the row at which the mouse
// stops finding transcript content. One derivation, so the two cannot drift — and the composed
// frame still fits the terminal.
//
// What View draws on that boundary row is the frame's blank gap row: the transcript-side slot sits
// BELOW the gap so its panes seat flush on the ▔ hairline, which puts the pane's first line one row
// further down. The gap row belongs to neither side — it maps to no content line, exactly as the
// pane rows do — so the seam is unmoved by where it falls.
func TestFrameRowBoundaryAgreesWithTheMouseMapping(t *testing.T) {
	deepTranscript := func(m *Model) {
		for i := range 60 {
			m.transcript.commitAssistant(fmt.Sprintf("reply line %02d", i), runRef{})
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

			// View's own boundary: the gap row is painted on exactly that row, and the overlay's
			// first line on the one below it.
			frame := strings.Split(m.View().Content, "\n")
			if overlay != "" {
				want := strings.Split(overlay, "\n")[0]
				if drawn+1 >= len(frame) {
					t.Fatalf("frame has %d rows, too short to hold the boundary row %d and the pane below it", len(frame), drawn)
				}
				if got := ansiPattern.ReplaceAllString(frame[drawn], ""); strings.TrimSpace(got) != "" {
					t.Errorf("frame row %d = %q, want the frame's blank gap row on the transcript's boundary", drawn, got)
				}
				if frame[drawn+1] != want {
					t.Errorf("frame row %d = %q, want the overlay's first line %q",
						drawn+1, ansiPattern.ReplaceAllString(frame[drawn+1], ""), ansiPattern.ReplaceAllString(want, ""))
				}
			}
			if len(frame) > m.height {
				t.Errorf("composed frame is %d rows on a %d-row terminal", len(frame), m.height)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Mouse in the /settings pane (mouse.go)
// ----------------------------------------------------------------------------

// settingsFrameCell is where want is PAINTED in the composed frame: the screen row it lands on and the
// display column its first cell occupies. A settings-mouse test aims at it rather than at coordinates
// of its own, so what the pointer is told to hit is what the frame actually drew there — the mapping
// under test never gets to define its own target. The column is measured in the width authority (the
// painter's, ADR 0030) because that is what a terminal reports a click in.
func settingsFrameCell(t *testing.T, m Model, want string) (x, y int) {
	t.Helper()
	frame := plain(m.View())
	for row, line := range strings.Split(frame, "\n") {
		if i := strings.Index(line, want); i >= 0 {
			return m.th.measure.Width(line[:i]), row
		}
	}
	t.Fatalf("no frame row paints %q:\n%s", want, frame)
	return 0, 0
}

// A click selects the key under the pointer, at every window the pane is drawn in — including the
// short one where the list is scrolled, which is exactly where a mapping that re-derived the pane's
// geometry instead of reading the painter's would name the wrong row. A click on a section label, on
// the spacer above it or on the pane's own chrome selects nothing: they are rows no keypress can put
// the ❯ on either.
func TestSettingsClickSelectsTheRowUnderThePointer(t *testing.T) {
	for _, height := range []int{24, 30, 40} {
		t.Run(fmt.Sprintf("%d rows", height), func(t *testing.T) {
			m := settingsFrameModel(t, 80, height, 8)
			x, y := settingsFrameCell(t, m, "key-02")

			m = step(t, m, leftClick(x, y))
			if m.settings.selected != 2 {
				t.Fatalf("selected = %d, want the row the pointer was on (2)", m.settings.selected)
			}
			// The pane claims its own rows: no prompt or transcript selection is armed under it.
			if m.sel.active || m.transcriptSel.active {
				t.Errorf("a click on the pane armed another surface's selection: %+v / %+v", m.sel, m.transcriptSel)
			}

			labelX, labelY := settingsFrameCell(t, m, "Interface")
			if onLabel := step(t, m, leftClick(labelX, labelY)); onLabel.settings.selected != 2 {
				t.Errorf("a click on the %q section label moved the selection to %d", "Interface", onLabel.settings.selected)
			}
			hintX, hintY := settingsFrameCell(t, m, settingsHint)
			if onHint := step(t, m, leftClick(hintX, hintY)); onHint.settings.selected != 2 {
				t.Errorf("a click on the pane's legend moved the selection to %d", onHint.settings.selected)
			}
		})
	}
}

// A click in the open edit buffer seats the caret at the glyph under the pointer — the /sessions
// rename idiom's field with a pointer on it (spec requirement 7) — and arms a collapsed selection
// there, exactly as a click in the prompt does. The CJK case is the point of the conversion: a column
// is display cells and the caret is a rune offset, so a value of two-cell glyphs is where a mapping
// that counted runes would land one glyph out.
func TestSettingsClickSeatsTheCaretInTheEditField(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		want    string // the run the pointer is put on
		wantOff int    // the rune offset the caret must land at
	}{
		{"ascii", "http://box:1111", "box", 7},
		{"double-width glyphs", "日本語abc", "本", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := settingsStringRow()
			row.Value = c.value
			m, _ := settingsEditModel(t, []SettingRow{row}, &settingsWriteLog{})

			m = step(t, m, keyEnter()) // the buffer opens seeded with the value, caret at its end
			if m.settings.kind != settingsValueBuffer {
				t.Fatalf("pane = %+v, want the edit buffer open", m.settings)
			}
			x, y := settingsFrameCell(t, m, c.want)

			m = step(t, m, leftClick(x, y))

			if got := m.settings.editor.caretRune(); got != c.wantOff {
				t.Errorf("caret at rune %d, want %d (the %q the pointer was on)", got, c.wantOff, c.want)
			}
			if !m.settings.sel.active || m.settings.sel.anchorOff != c.wantOff || m.settings.sel.headOff != c.wantOff {
				t.Errorf("a bare click should arm a collapsed selection at %d, got %+v", c.wantOff, m.settings.sel)
			}
			if m.settings.editor.value() != c.value {
				t.Errorf("the click changed the field's value to %q", m.settings.editor.value())
			}
		})
	}
}

// A drag inside the edit row selects the value's text and the release copies exactly those runes — the
// prompt's own release, one surface along. The drag's target is found in the frame the PRESS left
// behind, because the caret glyph stands in the painted cell and moves the text under it: what the
// human aims at is what is on the screen at the moment they aim.
func TestSettingsDragSelectsAndCopies(t *testing.T) {
	m, _ := settingsEditModel(t, []SettingRow{settingsStringRow()}, &settingsWriteLog{})
	m = step(t, m, keyEnter())

	x, y := settingsFrameCell(t, m, "http://box:1111")
	m = step(t, m, leftClick(x, y)) // anchor on the first rune
	if m.settings.sel.anchorOff != 0 {
		t.Fatalf("anchor at %d, want the value's first rune", m.settings.sel.anchorOff)
	}
	head, _ := settingsFrameCell(t, m, "://box")
	m = step(t, m, leftDrag(head, y))

	if got := m.settings.sel.headOff; got != 4 {
		t.Fatalf("head at %d, want 4 — the drag ran over %q", got, "http")
	}
	if got := selectionText(m.settings.editor.value(), m.settings.sel.anchorOff, m.settings.sel.headOff); got != "http" {
		t.Fatalf("selected text = %q, want %q", got, "http")
	}
	if colorActive(m.th) {
		line := popupLineWith(t, m.renderSettings(), "http")
		if !strings.Contains(line, styleSGR(m.th.selection)) {
			t.Errorf("the dragged span is not shaded on the row: %q", line)
		}
	}

	m, cmd := stepCmd(t, m, leftRelease(head, y))
	if cmd == nil {
		t.Fatal("release of a non-empty selection should return a copy Cmd, got nil")
	}
	if !strings.Contains(m.flash, "copied 4 chars") {
		t.Fatalf("flash = %q, want it to mention 'copied 4 chars'", m.flash)
	}

	// A keypress moves on from the span, the prompt's rule at this pane's own chokepoint.
	if typed := step(t, m, keyRune('x')); typed.settings.sel.active {
		t.Errorf("a keypress left the drag-selection armed: %+v", typed.settings.sel)
	}
}

// The wheel walks the key list under the pointer, one key per notch, and CLAMPS at both ends where the
// arrows wrap — a scroll gesture must not land the human on the far end of the list. A notch outside
// the pane is the transcript's, which is what keeps the conversation above a short pane scrollable.
func TestSettingsWheelWalksTheKeyList(t *testing.T) {
	m := settingsFrameModel(t, 80, 30, 8)
	_, y := settingsFrameCell(t, m, "key-00")
	wheel := func(m Model, button tea.MouseButton, y int) Model {
		return step(t, m, tea.MouseWheelMsg{X: 10, Y: y, Button: button})
	}

	m = wheel(m, tea.MouseWheelDown, y)
	if m.settings.selected != 1 {
		t.Fatalf("selected = %d after one notch down, want 1", m.settings.selected)
	}
	m = wheel(wheel(m, tea.MouseWheelUp, y), tea.MouseWheelUp, y)
	if m.settings.selected != 0 {
		t.Fatalf("selected = %d after rolling past the top, want it clamped at 0", m.settings.selected)
	}
	for range 12 {
		m = wheel(m, tea.MouseWheelDown, y)
	}
	if m.settings.selected != 7 {
		t.Fatalf("selected = %d after rolling past the end, want it clamped at the last key (7)", m.settings.selected)
	}

	// Above the pane the transcript still owns the wheel.
	paneTop, _, ok := m.settingsPaneRect()
	if !ok {
		t.Fatal("the pane is not on the frame")
	}
	if paneTop > 0 {
		if off := wheel(m, tea.MouseWheelUp, paneTop-1); off.settings.selected != 7 {
			t.Errorf("a notch above the pane moved the selection to %d", off.settings.selected)
		}
	}
}

// settingsTextEditModel is a model with the multi-line field OPEN over the given prose — the state the
// pointer tests below act in. The prose is the row's own, so what the field is seeded with is what the
// registry would have handed it.
func settingsTextEditModel(t *testing.T, prose string) Model {
	t.Helper()
	row := settingsTextRow()
	row.Text = prose
	m, _ := settingsEditModel(t, []SettingRow{row}, &settingsWriteLog{})
	m = step(t, m, keyEnter())
	if m.settings.kind != settingsTextEditor {
		t.Fatalf("pane = %+v, want the multi-line field open", m.settings)
	}
	return m
}

// A click in the multi-line field seats the caret at the glyph under the pointer, wherever in the prose
// that is (spec requirement 7 — the same mouse the prompt box has). The three cases are the three ways a
// painted line can differ from the value's own: a line of its own, a line the pane had to WRAP — where
// the break dropped a blank that is still in the value — and a line of two-cell glyphs, where a mapping
// that counted runes rather than display cells would land one glyph out.
func TestSettingsTextClickSeatsTheCaretInTheProse(t *testing.T) {
	cases := []struct {
		name    string
		prose   string
		want    string // the run the pointer is put on
		wantOff int    // the rune offset into the prose the caret must land at
		wrapped bool   // the clicked line is one the pane had to break
	}{
		{"a line of its own", "You are apogee.\nWork step by step.", "step by", 21, false},
		{"a wrapped continuation", "You are apogee.\n" + strings.Repeat("alpha ", 20) + "omega.", "omega.", 136, true},
		{"double-width glyphs", "You are apogee.\n日本語abc", "本", 17, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := settingsTextEditModel(t, c.prose)
			paint, ok := m.settingsTextPaint()
			if !ok {
				t.Fatal("the field is not on the frame")
			}
			if wrapped := len(paint.blocks[1]) > 1; wrapped != c.wrapped {
				t.Fatalf("the prose's second line paints on %d rows, want wrapped = %v", len(paint.blocks[1]), c.wrapped)
			}
			x, y := settingsFrameCell(t, m, c.want)

			m = step(t, m, leftClick(x, y))

			if got := m.settings.editor.caretRune(); got != c.wantOff {
				t.Errorf("caret at rune %d, want %d (the %q the pointer was on)", got, c.wantOff, c.want)
			}
			if !m.settings.sel.active || m.settings.sel.anchorOff != c.wantOff || m.settings.sel.headOff != c.wantOff {
				t.Errorf("a bare click should arm a collapsed selection at %d, got %+v", c.wantOff, m.settings.sel)
			}
			if m.settings.editor.value() != c.prose {
				t.Errorf("the click changed the prose to %q", m.settings.editor.value())
			}
			if m.sel.active || m.transcriptSel.active {
				t.Errorf("a click on the field armed another surface's selection: %+v / %+v", m.sel, m.transcriptSel)
			}
		})
	}
}

// A drag in the multi-line field selects ACROSS its lines — the newline between them included, since
// that is what the value holds — the span is shaded on every line it covers, and the release copies
// exactly those runes.
func TestSettingsTextDragSelectsAcrossLines(t *testing.T) {
	m := settingsTextEditModel(t, "You are apogee.\nWork step by step.")

	x, y := settingsFrameCell(t, m, "You are apogee.")
	m = step(t, m, leftClick(x, y))
	if m.settings.sel.anchorOff != 0 {
		t.Fatalf("anchor at %d, want the prose's first rune", m.settings.sel.anchorOff)
	}
	head, headY := settingsFrameCell(t, m, "step by")
	m = step(t, m, leftDrag(head, headY))

	const want = "You are apogee.\nWork "
	if got := selectionText(m.settings.editor.value(), m.settings.sel.anchorOff, m.settings.sel.headOff); got != want {
		t.Fatalf("selected text = %q, want %q", got, want)
	}
	if colorActive(m.th) {
		for _, line := range []string{"You are apogee.", "Work "} {
			if row := popupLineWith(t, m.renderSettings(), line); !strings.Contains(row, styleSGR(m.th.selection)) {
				t.Errorf("the dragged span is not shaded on the line holding %q: %q", line, row)
			}
		}
	}

	m, cmd := stepCmd(t, m, leftRelease(head, headY))
	if cmd == nil {
		t.Fatal("release of a non-empty selection should return a copy Cmd, got nil")
	}
	if flash := fmt.Sprintf("copied %d chars", len([]rune(want))); !strings.Contains(m.flash, flash) {
		t.Fatalf("flash = %q, want %q", m.flash, flash)
	}
}

// The wheel walks the multi-line field one prose line per notch — the caret is what its scroll window
// follows, so moving the caret IS the scroll — and clamps at the first and last lines, where a wheel
// must not roll round. A notch above the pane is still the transcript's.
func TestSettingsTextWheelWalksTheProse(t *testing.T) {
	lines := make([]string, 12)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%02d", i)
	}
	m := settingsTextEditModel(t, strings.Join(lines, "\n"))
	if got := m.settings.editor.caretLine(); got != len(lines)-1 {
		t.Fatalf("the field opened with the caret on line %d, want the last (%d)", got, len(lines)-1)
	}
	paneTop, _, ok := m.settingsPaneRect()
	if !ok {
		t.Fatal("the pane is not on the frame")
	}
	wheel := func(m Model, button tea.MouseButton, y int) Model {
		return step(t, m, tea.MouseWheelMsg{X: 10, Y: y, Button: button})
	}
	inside := paneTop + 1

	m = wheel(m, tea.MouseWheelUp, inside)
	if got := m.settings.editor.caretLine(); got != len(lines)-2 {
		t.Fatalf("caret on line %d after one notch up, want %d", got, len(lines)-2)
	}
	for range len(lines) + 4 {
		m = wheel(m, tea.MouseWheelUp, inside)
	}
	if got := m.settings.editor.caretLine(); got != 0 {
		t.Fatalf("caret on line %d after rolling past the top, want it clamped at 0", got)
	}
	for range len(lines) + 4 {
		m = wheel(m, tea.MouseWheelDown, inside)
	}
	if got := m.settings.editor.caretLine(); got != len(lines)-1 {
		t.Fatalf("caret on line %d after rolling past the end, want it clamped at %d", got, len(lines)-1)
	}
	if m.settings.editor.value() != strings.Join(lines, "\n") {
		t.Errorf("the wheel changed the prose to %q", m.settings.editor.value())
	}
	if paneTop > 0 {
		above := wheel(m, tea.MouseWheelUp, paneTop-1)
		if got := above.settings.editor.caretLine(); got != len(lines)-1 {
			t.Errorf("a notch above the pane moved the caret to line %d", got)
		}
	}
}

// The transcript above a short pane keeps its drag while the pane's edit field holds a highlight of its
// own — the pane claims only its OWN rows, in BOTH directions. The direction this guards is the one a
// live selection makes silent: a settings span left armed answers every motion and every release from
// then on, so a click the pane did not claim has to drop it, exactly as the transcript's and the
// prompt's are dropped when another surface takes a click. Without that, a drag over the conversation
// moves nothing and its release copies the field's stale runes.
func TestTranscriptDragOutlivesASettingsHighlight(t *testing.T) {
	m := settingsFrameModel(t, 80, 30, 8)
	m = step(t, m, keyEnter()) // the buffer opens on key-00, seeded with its value

	// A real, non-empty highlight in the field — the state that used to swallow everything after it.
	fieldX, fieldY := settingsFrameCell(t, m, "value-00")
	m = step(t, m, leftClick(fieldX, fieldY))
	m = step(t, m, leftDrag(fieldX+4, fieldY))
	if !m.settings.sel.active || m.settings.sel.anchorOff == m.settings.sel.headOff {
		t.Fatalf("the field holds no drag-selection to guard against: %+v", m.settings.sel)
	}

	row := m.transcriptRows() - 1 // the last conversation row, immediately above the pane
	if _, _, ok := m.pointTranscriptRow(0, row); !ok {
		t.Fatalf("row %d is not a transcript row on this frame", row)
	}

	m = step(t, m, leftClick(2, row))
	if m.settings.sel.active {
		t.Fatalf("a click the pane did not claim left its highlight armed: %+v", m.settings.sel)
	}
	m = step(t, m, leftDrag(20, row))
	if !m.transcriptSel.active || m.transcriptSel.head.col != 20 {
		t.Fatalf("transcript selection = %+v, want its head at the dragged cell (col 20)", m.transcriptSel)
	}

	want := transcriptSelectionText(m.th.measure, m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
	if len([]rune(want)) <= 4 {
		t.Fatalf("the transcript span is %q, too short to tell from the field's stale one", want)
	}
	m, cmd := stepCmd(t, m, leftRelease(20, row))
	if cmd == nil {
		t.Fatal("release of a non-empty transcript selection should return a copy Cmd, got nil")
	}
	if flash := fmt.Sprintf("copied %d chars", len([]rune(want))); !strings.Contains(m.flash, flash) {
		t.Fatalf("flash = %q, want %q — the transcript's own span, not the field's", m.flash, flash)
	}
}

// modelWithSuperGroup builds a ready idle model holding one user prompt and four calls in two runs
// of different labels — two reads, then two Runs each carrying output — which is exactly what the
// transcript folds into an umbrella (toolSuperGroup). The Runs carry bodies so their members have
// something to reveal and therefore states of their own. The start-up box is dropped so the block
// sits high enough to be aimed at.
func modelWithSuperGroup(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser("read the files, then check the build", nil)
	readCall(&m.transcript, "c1", "a.go", 1, 5, 0)
	readCall(&m.transcript, "c2", "b.go", 1, 9, 0)
	runCall(&m.transcript, "c3", "go build ./...", "ok\nbuilt", 0)
	runCall(&m.transcript, "c4", "go test ./...", "ok\nPASS", 0)
	m.refreshViewport()
	return m
}

// typeRowLine is the content line the run headed by entry head is listed on inside its umbrella,
// read off the marks the painter made — the same accounting the mouse resolves against.
func typeRowLine(t *testing.T, m Model, head int) int {
	t.Helper()
	for i, target := range m.lineTargets {
		if target.kind == targetType && target.entry == head {
			return i
		}
	}
	t.Fatalf("no rendered row is marked as the type row of entry %d:\n%s", head, strings.Join(m.lines, "\n"))
	return -1
}

// umbrellaHeaderLine is the content line the umbrella's own header paints on. It is found by its
// TEXT rather than by its mark, because the header is deliberately unmarked while nothing is open —
// which is one of the things the test below asserts.
func umbrellaHeaderLine(t *testing.T, m Model) int {
	t.Helper()
	for i, line := range m.lines {
		if strings.Contains(strip(line), superGroupLabel+" (") {
			return i
		}
	}
	t.Fatalf("the transcript paints no umbrella header:\n%s", strings.Join(m.lines, "\n"))
	return -1
}

// TestSuperGroupClickTogglesEachLevel is the umbrella's whole interaction (design calls 6 and 9):
// the deepest element under the pointer wins, so a click on a type row opens that RUN to its member
// rows and a click on a member opens that CALL to its body, each leaving the other level alone; and
// a click on the umbrella header — which toggles nothing, its floor being the type rows — closes
// every open child at once.
func TestSuperGroupClickTogglesEachLevel(t *testing.T) {
	// entries[0] is the prompt, so the two reads are entries 1..2 and the two Runs 3..4: one umbrella
	// of two runs, headed at 1 and 3.
	const readRun, runRun = 1, 3

	clickLine := func(t *testing.T, m Model, line int) Model {
		t.Helper()
		return clickCell(t, m, 4, screenRow(t, m, line))
	}

	t.Run("a click on a type row lists the calls behind it", func(t *testing.T) {
		m := modelWithSuperGroup(t)
		m = clickLine(t, m, typeRowLine(t, m, readRun))
		if !m.transcript.entries[readRun].typeExpanded {
			t.Fatal("a click on the Read type row did not open it")
		}
		if m.transcript.entries[runRun].typeExpanded {
			t.Error("opening one type row opened the other as well")
		}
		body := strings.Join(m.lines, "\n")
		for _, want := range []string{"a.go", "b.go"} {
			if !strings.Contains(body, want) {
				t.Errorf("the open run never listed %q:\n%s", want, body)
			}
		}
		if m = clickLine(t, m, typeRowLine(t, m, readRun)); m.transcript.entries[readRun].typeExpanded {
			t.Error("a second click on the type row did not close it again")
		}
	})

	t.Run("a click on a member row opens that call alone", func(t *testing.T) {
		m := modelWithSuperGroup(t)
		m = clickLine(t, m, typeRowLine(t, m, runRun))
		m = clickLine(t, m, memberRows(t, m, runRun)[0])
		if !m.transcript.entries[runRun].expanded {
			t.Fatal("a click on the member row did not open the call")
		}
		if m.transcript.entries[runRun+1].expanded {
			t.Error("opening one member opened its sibling as well")
		}
		if !m.transcript.entries[runRun].typeExpanded {
			t.Error("opening a member closed the type row it sits under")
		}
		if body := strings.Join(m.lines, "\n"); !strings.Contains(body, "built") {
			t.Errorf("the open member's body never reached the viewport:\n%s", body)
		}
	})

	t.Run("the header closes every open child", func(t *testing.T) {
		m := modelWithSuperGroup(t)
		m = clickLine(t, m, typeRowLine(t, m, readRun))
		m = clickLine(t, m, typeRowLine(t, m, runRun))
		m = clickLine(t, m, memberRows(t, m, runRun)[0])

		m = clickLine(t, m, umbrellaHeaderLine(t, m))
		for i, e := range m.transcript.entries {
			if e.expanded || e.typeExpanded {
				t.Errorf("entry %d is still open after the header click (expanded=%v, type=%v)",
					i, e.expanded, e.typeExpanded)
			}
		}
	})

	t.Run("the header offers nothing while nothing is open", func(t *testing.T) {
		m := modelWithSuperGroup(t)
		header := umbrellaHeaderLine(t, m)
		if kind := m.lineTargets[header].kind; kind != targetNone {
			t.Errorf("the header of a shut umbrella is marked %v, want no target at all", kind)
		}
		before := strings.Join(m.lines, "\n")
		if got := strings.Join(clickLine(t, m, header).lines, "\n"); got != before {
			t.Errorf("a click on the shut umbrella's header repainted it:\n%s", got)
		}
	})
}

// modelWithSubAgentGroup builds a ready idle model whose scrollback holds a fan-out of three
// delegations, each with one nested read behind it — the shape the folded "✦ Sub-Agent (3)" list is
// derived from (subAgentGroup). The delegations stand at entries 1, 3 and 5, the prompt at 0.
func modelWithSubAgentGroup(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser("survey the repo three ways", nil)
	for _, d := range [][3]string{
		{"s1", "survey", "a.go"},
		{"s2", "build", "b.go"},
		{"s3", "check", "c.go"},
	} {
		subAgentCall(&m.transcript, d[0], d[1], 0)
		readCall(&m.transcript, "r"+d[0], d[2], 1, 5, 1)
		subAgentReport(&m.transcript, d[0], "all clear", 0)
	}
	m.refreshViewport()
	return m
}

// TestSubAgentGroupMemberClickOpensItsSpan is the folded list's interaction: a member row is its own
// click surface, so a click opens THAT delegation's span and leaves its siblings folded, and a
// second click on the same row closes it again. The group header itself toggles nothing — a list
// has no state of its own, exactly as the same-label group's header has none.
func TestSubAgentGroupMemberClickOpensItsSpan(t *testing.T) {
	// The prompt is entries[0], so the three delegations head at 1, 3 and 5.
	const first, middle, last = 1, 3, 5

	clickLine := func(t *testing.T, m Model, line int) Model {
		t.Helper()
		return clickCell(t, m, 4, screenRow(t, m, line))
	}

	t.Run("a click opens the delegation it landed on, alone", func(t *testing.T) {
		m := modelWithSubAgentGroup(t)
		m = clickLine(t, m, memberRows(t, m, middle)[0])
		if !m.transcript.entries[middle].expanded {
			t.Fatal("a click on the middle delegation's row did not open it")
		}
		for _, sibling := range []int{first, last} {
			if m.transcript.entries[sibling].expanded {
				t.Errorf("opening entry %d opened entry %d as well", middle, sibling)
			}
		}
		body := strings.Join(m.lines, "\n")
		if !strings.Contains(body, "b.go") {
			t.Errorf("the open delegation's span never reached the viewport:\n%s", body)
		}
		if strings.Contains(body, "a.go") || strings.Contains(body, "c.go") {
			t.Errorf("a sibling's span came with it:\n%s", body)
		}
	})

	t.Run("a second click closes it again", func(t *testing.T) {
		m := modelWithSubAgentGroup(t)
		m = clickLine(t, m, memberRows(t, m, middle)[0])
		if m = clickLine(t, m, memberRows(t, m, middle)[0]); m.transcript.entries[middle].expanded {
			t.Error("a second click on the open delegation's row did not close it")
		}
	})

	t.Run("the group header toggles nothing", func(t *testing.T) {
		m := modelWithSubAgentGroup(t)
		line := memberRows(t, m, first)[0] - 1 // the header sits directly above the first row
		if got := strip(m.lines[line]); !strings.Contains(got, "Sub-Agent (3)") {
			t.Fatalf("setup: the line above the first member is %q, not the group header", got)
		}
		if kind := m.lineTargets[line].kind; kind != targetNone {
			t.Errorf("the group header is marked %v, want no target at all", kind)
		}
		before := strings.Join(m.lines, "\n")
		if got := strings.Join(clickLine(t, m, line).lines, "\n"); got != before {
			t.Errorf("a click on the group header repainted it:\n%s", got)
		}
	})
}

// ----------------------------------------------------------------------------
// Mouse in the /usage report (usage.go)
// ----------------------------------------------------------------------------

// usageReportModel opens the report over more delegates than the pane can seat rows for, which is the
// state both the hit-test and the scroll below are about.
func usageReportModel(t *testing.T, delegates int) Model {
	t.Helper()
	m := usageModel(t, mainTotals, 8192)
	for i := range delegates {
		m = delegate(t, m, fmt.Sprintf("s%d", i), fmt.Sprintf("survey %d", i), childTotals, 0)
	}
	m.layout()
	return m
}

// A click ON the report is swallowed — it has nothing to select, and a press that armed a drag would
// take the transcript lines hidden under the pane — while a click anywhere else dismisses it. That
// second click still does what it was aimed at: the pane is not modal (layout.md), so the caret is
// seated in the prompt exactly as it would have been with no report up.
func TestUsageReportUnderTheClick(t *testing.T) {
	m := usageReportModel(t, 20)
	paneTop, h, ok := m.usagePaneRect()
	if !ok {
		t.Fatal("the report is not on the frame")
	}

	inside := step(t, m, leftClick(10, paneTop+h/2))
	if !inside.usagePane.open {
		t.Error("a click on the report closed it")
	}
	if inside.sel.active || inside.transcriptSel.active {
		t.Errorf("a click on the report armed a selection beneath it: prompt %+v, transcript %+v",
			inside.sel, inside.transcriptSel)
	}

	_, inputTop, _, _ := m.inputContentRect()
	outside := step(t, m, leftClick(4, inputTop))
	if outside.usagePane.open {
		t.Error("a click outside the report left it up")
	}
	if !outside.sel.active {
		t.Error("the dismissing click was swallowed; it should still seat the caret it was aimed at")
	}
}

// The wheel scrolls the report one row per notch while the pointer is over it and CLAMPS at both ends
// — rolling past the last row must not land the reader back on the first — and the last window it
// reaches is a FULL one, the end of the list against the bottom of the pane. A notch outside the pane
// is the transcript's, which is what keeps the conversation behind the report scrollable.
func TestUsageWheelScrollsTheReport(t *testing.T) {
	m := usageReportModel(t, 20)
	paneTop, h, ok := m.usagePaneRect()
	if !ok {
		t.Fatal("the report is not on the frame")
	}
	win, ok := m.usageWindow()
	if !ok {
		t.Fatal("the report reports no window")
	}
	if win.start != 0 || win.end >= win.total {
		t.Fatalf("precondition: window [%d,%d) of %d rows — the report must open at the top with rows below it",
			win.start, win.end, win.total)
	}
	seats := win.end - win.start
	wheel := func(m Model, button tea.MouseButton, y int) Model {
		return step(t, m, tea.MouseWheelMsg{X: 10, Y: y, Button: button})
	}
	y := paneTop + h/2

	down := wheel(m, tea.MouseWheelDown, y)
	if down.usagePane.top != 1 {
		t.Fatalf("top = %d after one notch down, want 1", down.usagePane.top)
	}
	if back := wheel(wheel(down, tea.MouseWheelUp, y), tea.MouseWheelUp, y); back.usagePane.top != 0 {
		t.Errorf("top = %d after rolling past the first row, want it clamped at 0", back.usagePane.top)
	}

	end := m
	for range win.total + 5 {
		end = wheel(end, tea.MouseWheelDown, y)
	}
	last, ok := end.usageWindow()
	if !ok {
		t.Fatal("the scrolled report reports no window")
	}
	if last.end != last.total {
		t.Errorf("scrolled to the end the window is [%d,%d) of %d rows, want it to reach the last row",
			last.start, last.end, last.total)
	}
	if got := last.end - last.start; got != seats {
		t.Errorf("the last window shows %d rows, want a full %d — the rows end at the pane's bottom", got, seats)
	}

	// Above the pane the transcript still owns the wheel.
	if paneTop > 0 {
		if off := wheel(down, tea.MouseWheelUp, paneTop-1); off.usagePane.top != down.usagePane.top {
			t.Errorf("a notch above the report scrolled it to %d", off.usagePane.top)
		}
	}
}

// ----------------------------------------------------------------------------
// Mouse in the /inspect pane (inspector.go)
// ----------------------------------------------------------------------------

// inspectorPaneModel opens the raw-protocol pane over more records than it can seat rows for, which
// is the state both the hit-test and the scroll below are about. The scroll starts at the TOP rather
// than where the verb opens it (runInspectCommand seats the last window), because a wheel test needs
// rows below the window to roll on to.
func inspectorPaneModel(t *testing.T, records int) Model {
	t.Helper()
	m := newTestModel(t)
	m.opts.Inspector = true
	for i := range records {
		m = m.foldEvent(wireEvent(domain.WireDirectionRequest, fmt.Sprintf(`{"n":%d}`, i), i, 0))
	}
	m.inspector = inspectorPane{open: true}
	m.layout()
	return m
}

// A click ON the pane is swallowed — it has nothing to select, and a press that armed a drag would
// take the transcript lines hidden under it — while a click anywhere else dismisses it. That second
// click still does what it was aimed at: the pane is not modal (layout.md), so the caret is seated in
// the prompt exactly as it would have been with no pane up.
func TestInspectorPaneUnderTheClick(t *testing.T) {
	m := inspectorPaneModel(t, 12)
	paneTop, h, ok := m.inspectorPaneRect()
	if !ok {
		t.Fatal("the pane is not on the frame")
	}

	inside := step(t, m, leftClick(10, paneTop+h/2))
	if !inside.inspector.open {
		t.Error("a click on the pane closed it")
	}
	if inside.sel.active || inside.transcriptSel.active {
		t.Errorf("a click on the pane armed a selection beneath it: prompt %+v, transcript %+v",
			inside.sel, inside.transcriptSel)
	}

	_, inputTop, _, _ := m.inputContentRect()
	outside := step(t, m, leftClick(4, inputTop))
	if outside.inspector.open {
		t.Error("a click outside the pane left it up")
	}
	if !outside.sel.active {
		t.Error("the dismissing click was swallowed; it should still seat the caret it was aimed at")
	}
}

// The wheel scrolls the record list one row per notch while the pointer is over it and CLAMPS at both
// ends — rolling past the last row must not land the reader back on the first — and the last window it
// reaches is a FULL one, the end of the list against the bottom of the pane. A notch outside the pane
// is the transcript's, which is what keeps the conversation behind the pane scrollable.
func TestInspectorWheelScrollsTheRecords(t *testing.T) {
	m := inspectorPaneModel(t, 12)
	paneTop, h, ok := m.inspectorPaneRect()
	if !ok {
		t.Fatal("the pane is not on the frame")
	}
	win, ok := m.inspectorWindow()
	if !ok {
		t.Fatal("the pane reports no window")
	}
	if win.start != 0 || win.end >= win.total {
		t.Fatalf("precondition: window [%d,%d) of %d rows — the pane must open at the top with rows below it",
			win.start, win.end, win.total)
	}
	seats := win.end - win.start
	wheel := func(m Model, button tea.MouseButton, y int) Model {
		return step(t, m, tea.MouseWheelMsg{X: 10, Y: y, Button: button})
	}
	y := paneTop + h/2

	down := wheel(m, tea.MouseWheelDown, y)
	if down.inspector.top != 1 {
		t.Fatalf("top = %d after one notch down, want 1", down.inspector.top)
	}
	if back := wheel(wheel(down, tea.MouseWheelUp, y), tea.MouseWheelUp, y); back.inspector.top != 0 {
		t.Errorf("top = %d after rolling past the first row, want it clamped at 0", back.inspector.top)
	}

	end := m
	for range win.total + 5 {
		end = wheel(end, tea.MouseWheelDown, y)
	}
	last, ok := end.inspectorWindow()
	if !ok {
		t.Fatal("the scrolled pane reports no window")
	}
	if last.end != last.total {
		t.Errorf("scrolled to the end the window is [%d,%d) of %d rows, want it to reach the last row",
			last.start, last.end, last.total)
	}
	if got := last.end - last.start; got != seats {
		t.Errorf("the last window shows %d rows, want a full %d — the rows end at the pane's bottom", got, seats)
	}

	// Above the pane the transcript still owns the wheel.
	if paneTop > 0 {
		if off := wheel(down, tea.MouseWheelUp, paneTop-1); off.inspector.top != down.inspector.top {
			t.Errorf("a notch above the pane scrolled it to %d", off.inspector.top)
		}
	}
}

// ----------------------------------------------------------------------------
// Mouse across the two panes that share the slot (mouse.go, handleMouseClick)
// ----------------------------------------------------------------------------

// bothPanesModel opens the /usage report and the /inspect pane TOGETHER — the one pair the frame
// draws in the same bottom-anchored slot — over a transcript long enough that the rows either pane
// gives back name real content. records sizes the ring, which is what decides how far the pane
// regrows once the report above it goes: many records and it takes the whole slot back, one and it
// stays about the height it already had.
func bothPanesModel(t *testing.T, records int) Model {
	t.Helper()
	m := usageModel(t, mainTotals, 8192)
	for i := range 20 {
		m.transcript.addUser(fmt.Sprintf("prompt %d", i), nil)
	}
	m.opts.Inspector = true
	for i := range records {
		m = m.foldEvent(wireEvent(domain.WireDirectionRequest, fmt.Sprintf(`{"n":%d}`, i), i, 0))
	}
	m.inspector = inspectorPane{open: true}
	m.refreshViewport()
	m.layout()
	return m
}

// A click in the band the /inspect pane grows into dismisses and nothing more. The slot is
// bottom-anchored, so dropping the report grows the pane UPWARD — past the rows the report was drawn
// on, since it was drawn shorter than its grant — and the chain asked the pre-click frame rather
// than the model the dismissal left: the blank gap row above the report is neither the regrown box's
// nor the transcript's.
func TestClickInTheBandTheInspectorGrowsIntoFallsThrough(t *testing.T) {
	m := bothPanesModel(t, 30)
	usageTop, _, ok := m.usagePaneRect()
	if !ok {
		t.Fatal("the report is not on the frame")
	}
	preTop, _, ok := m.inspectorPaneRect()
	if !ok {
		t.Fatal("the pane is not on the frame")
	}
	postTop, _, ok := m.dismissUsage().inspectorPaneRect()
	if !ok {
		t.Fatal("the pane leaves the frame when the report above it is dismissed")
	}
	y := usageTop - gapHeight // the blank gap row: no pane's, and no transcript row either
	if y < postTop || y >= preTop {
		t.Fatalf("precondition: the gap row %d is outside the band [%d,%d) the pane regrows into",
			y, postTop, preTop)
	}

	after := step(t, m, leftClick(10, y))

	if after.usagePane.open {
		t.Error("the click in the band left the report up")
	}
	if after.inspector.open {
		t.Error("the regrown /inspect pane claimed a click aimed at the gap row above the report")
	}
	if after.transcriptSel.active {
		t.Errorf("the click armed a transcript selection the pre-click frame showed no row for: %+v",
			after.transcriptSel)
	}
	if after.sel.active {
		t.Errorf("the click armed a prompt selection: %+v", after.sel)
	}
}

// Inside the pane's PRE-CLICK rectangle the click is still the pane's: the report above it is
// dismissed under the same press — a click outside the report always dismisses it — and the box that
// answers is the one the human aimed at.
func TestClickInsideTheInspectorSurvivesTheReportDismissal(t *testing.T) {
	m := bothPanesModel(t, 30)
	paneTop, h, ok := m.inspectorPaneRect()
	if !ok {
		t.Fatal("the pane is not on the frame")
	}

	after := step(t, m, leftClick(10, paneTop+h/2))

	if !after.inspector.open {
		t.Error("a click inside the /inspect pane closed it")
	}
	if after.usagePane.open {
		t.Error("the click left the report up; a click outside the report dismisses it")
	}
	if after.sel.active || after.transcriptSel.active {
		t.Errorf("the click armed a selection beneath the pane: prompt %+v, transcript %+v",
			after.sel, after.transcriptSel)
	}
}

// A row the pre-click frame drew as chrome names no transcript line, whatever the frame left by the
// dismissal maps it to. Over a ring too short for the pane to regrow into them, the rows both panes
// give back go to the TRANSCRIPT — so the gap row above the report becomes a content row in the
// model the rest of the chain runs on, and selecting there would copy a line the human never saw at
// that Y.
func TestClickOnAVacatedRowSelectsNoTranscriptLine(t *testing.T) {
	m := bothPanesModel(t, 1)
	usageTop, _, ok := m.usagePaneRect()
	if !ok {
		t.Fatal("the report is not on the frame")
	}
	y := usageTop - gapHeight
	if _, _, ok := m.pointTranscriptRow(10, y); ok {
		t.Fatalf("precondition: the pre-click frame already names a transcript row at y=%d", y)
	}
	if _, _, ok := m.dismissUsage().dismissInspector().pointTranscriptRow(10, y); !ok {
		t.Fatalf("precondition: y=%d is no transcript row once both panes are dismissed either", y)
	}

	after := step(t, m, leftClick(10, y))

	if after.usagePane.open || after.inspector.open {
		t.Errorf("the click left a pane up: report %v, inspector %v", after.usagePane.open, after.inspector.open)
	}
	if after.transcriptSel.active {
		t.Errorf("the click selected a transcript row the pre-click frame drew as chrome: %+v",
			after.transcriptSel)
	}
}

// ----------------------------------------------------------------------------
// The frame's published geometry (model.go, frameSpans)
// ----------------------------------------------------------------------------

// TestPublishedFrameSpansMatchAFreshComposition pins the one thing publishing the geometry onto the
// pre-gesture snapshot is allowed to change: nothing. A Model carrying a composed frame answers every
// rectangle exactly as the same Model composing one on demand does — otherwise the saving would have
// bought a click a frame of its own.
func TestPublishedFrameSpansMatchAFreshComposition(t *testing.T) {
	m := bothPanesModel(t, 30)

	published := m.withFrameSpans()
	for p := framePane(0); p < paneKinds; p++ {
		wantY, wantH, wantOK := m.frameSpans().pane(p)
		gotY, gotH, gotOK := published.frameSpans().pane(p)
		if gotY != wantY || gotH != wantH || gotOK != wantOK {
			t.Errorf("pane %d reads (%d, %d, %v) off the published frame, want (%d, %d, %v)",
				p, gotY, gotH, gotOK, wantY, wantH, wantOK)
		}
	}
}

// TestTheClickChainKeepsItsFrameToItself is the other half of that bargain. The published spans
// describe ONE frame and are exactly as current as the gesture aimed at it, so they ride the pre-click
// snapshot and nothing else: a model that carried them back to Bubble Tea would answer the NEXT click
// with the geometry of a frame the human is no longer looking at.
func TestTheClickChainKeepsItsFrameToItself(t *testing.T) {
	m := bothPanesModel(t, 30)
	usageTop, _, ok := m.usagePaneRect()
	if !ok {
		t.Fatal("the report is not on the frame")
	}

	for _, tc := range []struct {
		name string
		y    int
	}{
		{"a click the report claims", usageTop},
		{"a click that falls through to the transcript", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after := step(t, m, leftClick(10, tc.y))
			if after.spans.composed {
				t.Error("the model returned to Bubble Tea carries the click's frame onward")
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Mouse in the /sessions browser (sessions.go)
// ----------------------------------------------------------------------------

// browserPaneModel opens the /sessions overlay over more records than the pane can seat rows for,
// with a screenful of transcript behind it. The transcript is the point: the browser is modal, so a
// notch it swallows has to be checked against lines that WOULD have scrolled had it fallen through.
func browserPaneModel(t *testing.T, sessions int) Model {
	t.Helper()
	m := streamOneScreen(t, newTestModel(t))
	m.sessionBrowser = browserWithSessions(sessions)
	m.layout()
	return m
}

// browserRect is where the open overlay is drawn: its top row, and a row squarely inside it for a
// notch to be aimed at.
func browserRect(t *testing.T, m Model) (paneTop, inside int) {
	t.Helper()
	y0, h, ok := m.frameSpans().pane(paneBrowser)
	if !ok {
		t.Fatal("the browser is not on the frame")
	}
	return y0, y0 + h/2
}

// wheelAt rolls one notch with the pointer on the given screen row.
func wheelAt(t *testing.T, m Model, button tea.MouseButton, y int) Model {
	t.Helper()
	return step(t, m, tea.MouseWheelMsg{X: 10, Y: y, Button: button})
}

// A notch over the pane walks the session list one row, which is the whole of the reported defect:
// before this the browser was one of the overlays a wheel fell straight through.
func TestBrowserWheelWalksTheSessionList(t *testing.T) {
	m := browserPaneModel(t, 12)
	_, y := browserRect(t, m)

	down := wheelAt(t, m, tea.MouseWheelDown, y)
	if down.sessionBrowser.selected != 1 {
		t.Fatalf("selected = %d after one notch down, want 1", down.sessionBrowser.selected)
	}

	back := wheelAt(t, wheelAt(t, down, tea.MouseWheelUp, y), tea.MouseWheelUp, y)
	if back.sessionBrowser.selected != 0 {
		t.Errorf("selected = %d after two notches up, want it back on the first row", back.sessionBrowser.selected)
	}
}

// The wheel CLAMPS where ↑/↓ WRAP, and the difference is the gesture rather than an inconsistency
// (ratified 2026-08-22): rolling past the last session and landing back on the first would move the
// human somewhere they did not aim. Both answers are asserted at both ends, so neither can drift
// into the other unnoticed.
func TestBrowserWheelClampsWhereTheKeysWrap(t *testing.T) {
	m := browserPaneModel(t, 12)
	_, y := browserRect(t, m)
	last := len(m.sessionBrowserView().rows) - 1
	if last < 2 {
		t.Fatalf("the pane offers %d rows; the clamp needs a list to walk", last+1)
	}

	if got := wheelAt(t, m, tea.MouseWheelUp, y).sessionBrowser.selected; got != 0 {
		t.Errorf("selected = %d after a notch up on the first row, want it clamped at 0", got)
	}
	if got := step(t, m, keyUp()).sessionBrowser.selected; got != last {
		t.Errorf("selected = %d after ↑ on the first row, want the keys to wrap to %d", got, last)
	}

	end := m
	for range last + 5 {
		end = wheelAt(t, end, tea.MouseWheelDown, y)
	}
	if end.sessionBrowser.selected != last {
		t.Errorf("selected = %d after rolling past the end, want it clamped at the last row (%d)",
			end.sessionBrowser.selected, last)
	}
	if got := step(t, end, keyDown()).sessionBrowser.selected; got != 0 {
		t.Errorf("selected = %d after ↓ on the last row, want the keys to wrap to 0", got)
	}
}

// Above the pane the transcript still owns the wheel: the browser claims the notches inside its
// rectangle and no others.
func TestBrowserWheelAboveThePaneScrollsTheTranscript(t *testing.T) {
	m := browserPaneModel(t, 12)
	paneTop, y := browserRect(t, m)
	if paneTop == 0 {
		t.Fatal("the pane starts on the first row; there is no transcript above it to aim at")
	}
	m = wheelAt(t, m, tea.MouseWheelDown, y) // somewhere for a stray notch to move the highlight from

	off := wheelAt(t, m, tea.MouseWheelUp, paneTop-1)
	if off.sessionBrowser.selected != m.sessionBrowser.selected {
		t.Errorf("a notch above the pane moved the highlight to %d, want it left at %d",
			off.sessionBrowser.selected, m.sessionBrowser.selected)
	}
	if off.viewport.YOffset() == m.viewport.YOffset() {
		t.Errorf("the transcript stayed at %d; a notch the pane does not own is the transcript's",
			off.viewport.YOffset())
	}
}

// A live rename edit or delete confirm is a modal surface within the modal: it owns the pane until
// it is answered, so a notch over it moves NOTHING — and, the browser being modal, still never
// reaches the transcript behind it.
func TestBrowserWheelIsSwallowedByARenameOrAConfirm(t *testing.T) {
	base := browserPaneModel(t, 12)
	_, y := browserRect(t, base)
	base = wheelAt(t, base, tea.MouseWheelDown, y) // off the first row, so a stray move would show

	for _, tc := range []struct {
		name string
		arm  func(Model) Model
	}{
		{"a rename edit", func(m Model) Model { m.sessionBrowser.renaming = true; return m }},
		{"a delete confirm", func(m Model) Model { m.sessionBrowser.confirming = true; return m }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.arm(base)

			after := wheelAt(t, m, tea.MouseWheelDown, y)

			if after.sessionBrowser.selected != m.sessionBrowser.selected {
				t.Errorf("selected = %d, want the armed surface to swallow the notch and leave it at %d",
					after.sessionBrowser.selected, m.sessionBrowser.selected)
			}
			if after.viewport.YOffset() != m.viewport.YOffset() {
				t.Errorf("the transcript scrolled to %d; a notch over the modal must never reach it",
					after.viewport.YOffset())
			}
		})
	}
}

// The notch walks the rows the pane PAINTS — the filtered view every key route and the painter share
// — so a wheel and an ↑ can never disagree about which record is highlighted, and the clamp is the
// filtered list's length rather than the store's.
func TestBrowserWheelWalksTheFilteredList(t *testing.T) {
	m := browserPaneModel(t, 12)
	for _, r := range "number 1" {
		m = step(t, m, keyRune(r))
	}
	rows := len(m.sessionBrowserView().rows)
	if rows != 2 {
		t.Fatalf("the filter leaves %d rows, want the 2 the clamp is measured against", rows)
	}
	_, y := browserRect(t, m)

	m = wheelAt(t, m, tea.MouseWheelDown, y)
	if m.sessionBrowser.selected != 1 {
		t.Fatalf("selected = %d after one notch down, want the second surviving row", m.sessionBrowser.selected)
	}

	if got := wheelAt(t, m, tea.MouseWheelDown, y).sessionBrowser.selected; got != 1 {
		t.Errorf("selected = %d after rolling past the filtered end, want it clamped at %d", got, rows-1)
	}
}
