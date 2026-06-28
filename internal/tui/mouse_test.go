package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
