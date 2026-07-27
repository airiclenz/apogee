package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The real terminal cursor (model.go promptCursor, prompteditor.go steadyCursor)
// ----------------------------------------------------------------------------
//
// apogee draws the terminal's OWN cursor at the prompt caret instead of painting a simulated
// blinking block into the textarea's content. Three things are worth pinning: the translation from
// the widget's coordinates to the screen (it must agree with inputContentRect, the rectangle a
// mouse click maps back through), the visibility rule (editability, not focus), and the steadiness
// — nothing blinks, ever.

// cursorModel builds a ready idle model whose Options carry shape, with value in the input box and
// the layout settled, so the box rectangle is final before View is asked for a cursor.
func cursorModel(t *testing.T, shape tea.CursorShape, value string) Model {
	t.Helper()
	opts := testOpts
	opts.CursorShape = shape
	m := newTestModelEng(t, &fakeEngine{}, opts) // 80x24
	m.input.SetValue(value)
	m.layout()
	return m
}

// The View carries a real cursor at the caret's absolute cell, in the configured shape, never
// blinking. The expected cell is the content rectangle's origin plus the caret's own offset —
// SetValue leaves the caret after the text, so "hello" puts it five cells in.
func TestViewCarriesRealCursorAtCaret(t *testing.T) {
	t.Parallel()
	m := cursorModel(t, tea.CursorBar, "hello")

	c := m.View().Cursor
	if c == nil {
		t.Fatal("View carried no cursor at idle with a focused input; the caret would be invisible")
	}
	x0, y0, _, _ := m.inputContentRect()
	if wantX, wantY := x0+len("hello"), y0; c.X != wantX || c.Y != wantY {
		t.Errorf("cursor at (%d,%d), want (%d,%d) — the box's content origin plus the caret offset",
			c.X, c.Y, wantX, wantY)
	}
	if c.Blink {
		t.Error("the cursor blinks; apogee's caret is always steady")
	}
	if c.Shape != tea.CursorBar {
		t.Errorf("cursor shape = %v, want the configured %v (Options.CursorShape)", c.Shape, tea.CursorBar)
	}
}

// The shape is a selection, not a default the renderer invents: each configured shape reaches the
// frame as itself, and the zero value (an unset `cursor-shape:`) is the block.
func TestCursorShapeFollowsOptions(t *testing.T) {
	t.Parallel()
	for _, shape := range []tea.CursorShape{tea.CursorBlock, tea.CursorUnderline, tea.CursorBar} {
		m := cursorModel(t, shape, "hi")
		c := m.View().Cursor
		if c == nil {
			t.Fatalf("no cursor for shape %v", shape)
		}
		if c.Shape != shape {
			t.Errorf("cursor shape = %v, want %v", c.Shape, shape)
		}
	}
}

// Visibility follows EDITABILITY (inputEditable): the caret shows where a keypress reaches the box
// — idle, the borrowed ask answer, and while a worker runs (ADR 0025) — and is hidden where the
// keyboard belongs to something else: an approval decision (a/d/s) and the errored dismiss. The
// textarea is never blurred, so this is a state rule; a caret in an inert box would invite typing
// that goes nowhere.
func TestCursorHiddenAtApprovalAndError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		state   uiState
		visible bool
	}{
		{stateIdle, true},
		{stateAwaitingAsk, true},
		{stateRunning, true},
		{stateAwaitingApproval, false},
		{stateErrored, false},
	}
	for _, tt := range tests {
		m := cursorModel(t, tea.CursorBlock, "typed")
		m.state = tt.state
		c := m.View().Cursor
		if tt.visible && c == nil {
			t.Errorf("state %v: no cursor, want one (the box is editable there)", tt.state)
		}
		if !tt.visible {
			if c != nil {
				t.Errorf("state %v: cursor at (%d,%d), want none (the box is inert there)", tt.state, c.X, c.Y)
			}
			if !m.input.Focused() {
				t.Errorf("state %v: the input was blurred; hiding the cursor must not cost the box its focus", tt.state)
			}
		}
	}
}

// A caret on the second VISUAL row of a soft-wrapped line lands one screen row lower: the
// translation adds the content rectangle's origin to the widget's own wrap-aware position, so a
// wrapped prompt draws its caret where the box actually shows it (and where a click there would
// put it — the same rectangle, caretTo).
func TestCursorFollowsMultilineCaret(t *testing.T) {
	t.Parallel()
	m := cursorModel(t, tea.CursorBlock, strings.Repeat("a", 200)) // wraps at the 80-column box

	x0, y0, _, h := m.inputContentRect()
	if h < 2 {
		t.Fatalf("value did not wrap: box height %d, want ≥2 visual rows", h)
	}
	m.caretTo(1, 0) // the widget's own wrap-aware walk: visual row 1, first cell

	c := m.View().Cursor
	if c == nil {
		t.Fatal("View carried no cursor with the caret on the second visual row")
	}
	if wantX, wantY := x0, y0+1; c.X != wantX || c.Y != wantY {
		t.Errorf("cursor at (%d,%d), want (%d,%d) — one row below the box's first content row",
			c.X, c.Y, wantX, wantY)
	}
}

// The widget's simulated cursor is retired at construction. This is what makes the frame's cursor
// the terminal's own: textarea.Cursor() answers nil while the virtual cursor is in use, and the
// widget would otherwise paint a blinking block into its content as well.
func TestVirtualCursorDisabled(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	if m.input.VirtualCursor() {
		t.Error("the textarea still uses its virtual cursor; the real terminal cursor would never be reported")
	}
	if s := m.input.Styles(); s.Cursor.Blink {
		t.Error("the textarea's cursor style still blinks; apogee's caret is steady in every state")
	}
}
