package tui

import (
	"reflect"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/scheme"
)

// ----------------------------------------------------------------------------
// promptEditor — editor-direct unit tests (review candidate #3)
// ----------------------------------------------------------------------------
//
// These exercise the promptEditor in isolation — no Model, no fake engine, no full Update loop —
// which is the payoff of lifting the input cluster into its own type: the self-contained input
// logic is now testable without standing up the whole widget graph. The same behaviour is also
// covered end-to-end through the Model in minilang_test.go / skill_test.go / mouse_test.go, which
// keep passing unmodified (the refactor's safety net); these add the direct, loop-free path.

// submitParse classifies a free-text line as a message and extracts its @file references.
func TestPromptEditorSubmitParseMessage(t *testing.T) {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
	e.input.SetValue("look at @main.go and @pkg/x.go please")
	parsed := e.submitParse(nil)
	if parsed.kind != kindMessage {
		t.Fatalf("kind = %v, want kindMessage", parsed.kind)
	}
	if want := "look at @main.go and @pkg/x.go please"; parsed.text != want {
		t.Errorf("text = %q, want %q (the @tokens stay in place)", parsed.text, want)
	}
	if want := []string{"main.go", "pkg/x.go"}; !reflect.DeepEqual(parsed.fileRefs, want) {
		t.Errorf("fileRefs = %v, want %v", parsed.fileRefs, want)
	}
	if len(parsed.skillIDs) != 0 {
		t.Errorf("skillIDs = %v, want none", parsed.skillIDs)
	}
}

// submitParse recognises a leading /command and reports the bare verb.
func TestPromptEditorSubmitParseCommand(t *testing.T) {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
	e.input.SetValue("/clear")
	parsed := e.submitParse(nil)
	if parsed.kind != kindCommand || parsed.command != "clear" {
		t.Fatalf("parsed = %+v, want kindCommand verb=clear", parsed)
	}
}

// submitParse resolves the inline /tokens through the predicate it is handed, so a message that
// names a skill arrives with the id extracted and the token still in its text.
func TestPromptEditorSubmitParseExtractsSkillTokens(t *testing.T) {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
	e.input.SetValue("/go-testing tidy this up")
	parsed := e.submitParse(knownSkills("go-testing", "git"))
	if want := "/go-testing tidy this up"; parsed.text != want {
		t.Errorf("text = %q, want %q (the /token stays in the message)", parsed.text, want)
	}
	if want := []string{"go-testing"}; !reflect.DeepEqual(parsed.skillIDs, want) {
		t.Errorf("skillIDs = %v, want %v", parsed.skillIDs, want)
	}
}

// reset empties every editable part of the editor: the textarea and the overlay. Emptying the text
// is what drops the skills too — they live in it as /tokens, not beside it.
func TestPromptEditorResetClearsEverything(t *testing.T) {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
	e.input.SetValue("half-typed /go")
	e.autocomplete = autocompleteState{active: true, kind: acCommand}
	e.reset()
	if v := e.input.Value(); v != "" {
		t.Errorf("input = %q, want empty after reset", v)
	}
	if e.autocomplete.active {
		t.Error("autocomplete still active after reset")
	}
	if got := e.submitParse(knownSkills("go")); len(got.skillIDs) != 0 {
		t.Errorf("skillIDs = %v, want none once the text is gone", got.skillIDs)
	}
}

// wrappedRowsOf reports how many wrapped sub-rows the WIDGET gives one logical line at width,
// read back through its own LineInfo with the caret parked on that line. It is deliberately not
// inputContentRows: that one is apogee's MIRROR of this answer (pinned to it by
// TestInputContentRowsMirrorsTheWidget), and a caret test that leaned on the mirror would go green
// on a geometry the widget never drew — including the phantom trailing sub-line bubbles appends to
// a line that fills its last row exactly, which is the very geometry the caret seat has to survive.
func wrappedRowsOf(line string, width int) int {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
	e.input.SetWidth(width)
	e.input.SetValue(line)
	e.input.MoveToBegin()
	return e.input.LineInfo().Height
}

// caretToOffset must reach a logical row that sits below a SOFT-WRAPPED one, and its walk must
// terminate whatever the geometry. bubbles' CursorDown steps one VISUAL row, so a wrapped line
// takes several steps to cross — and on a line that fills its last row exactly and ends with a
// space it takes INFINITELY many: the wrap appends a phantom trailing sub-line, and CursorDown's
// column clamp (len(line)-1) can never enter it, so the caret does not move at all. A seat built on
// bare CursorDowns therefore stalls on the first line forever and a completion spliced into the
// second seats its caret in the middle of the first. Every rune position of each draft round-trips.
func TestPromptEditorCaretToOffsetCrossesWrappedRows(t *testing.T) {
	cases := []struct {
		name  string
		width int
		value string
	}{
		// The pathological geometries: a first line that ends with a space exactly at a row
		// boundary, at the app's real text width (76) and the two others the walk failed at.
		{"phantom row at app width", 76, strings.Repeat("aaa ", 19) + "\nplease /rev"},
		{"phantom row at width 8", 8, strings.Repeat("wrapped ", 20) + "\nplease /rev"},
		{"phantom row at width 80", 80, strings.Repeat("wrapped ", 20) + "\nplease /rev"},
		// Three logical lines, the middle one phantom-wrapped: the walk crosses two of them.
		{"phantom row in the middle", 76, "head\n" + strings.Repeat("aaa ", 19) + "\ntail past it"},
		// An ordinarily wrapped line, and one with wide runes, still round-trip.
		{"plain wrap", 20, strings.Repeat("wrapped ", 12) + "\nsecond line, well past the wrap"},
		{"wide runes", 20, "日本語のテキストです ここに " + strings.Repeat("あ", 30) + "\nsecond 🎉 line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := false
			for _, line := range strings.Split(tc.value, "\n") {
				if wrappedRowsOf(line, tc.width) >= 2 {
					wrapped = true
				}
			}
			if !wrapped {
				t.Fatal("no logical line wraps at this width; the walk is never asked to cross one")
			}
			e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
			e.input.SetWidth(tc.width)
			e.input.SetHeight(3) // shorter than the draft, so the scroll re-clamp runs for real
			e.input.SetValue(tc.value)
			e.input.MoveToEnd()

			// Rune starts only: a byte offset inside a multi-byte rune is not a caret position.
			offs := []int{len(tc.value)}
			for i := range tc.value {
				offs = append(offs, i)
			}
			for _, off := range offs {
				e.caretToOffset(off)
				if got := e.caretByteOffset(); got != off {
					t.Fatalf("caretToOffset(%d) seated the caret at %d (row %d, col %d)",
						off, got, e.input.Line(), e.input.Column())
				}
				// The auto-grow re-clamp runs on the same seat and must not move the caret either.
				e.reseatInput()
				if got := e.caretByteOffset(); got != off {
					t.Fatalf("reseatInput moved the caret from %d to %d", off, got)
				}
			}

			// An offset past the end lands at the end rather than running away.
			e.caretToOffset(len(tc.value) + 99)
			if got := e.caretByteOffset(); got != len(tc.value) {
				t.Errorf("caretToOffset(past the end) = %d, want %d", got, len(tc.value))
			}
		})
	}
}

// rows grows one row per logical line and clamps at maxInputRows.
func TestPromptEditorRowsGrowsAndClamps(t *testing.T) {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))

	e.input.SetValue("hello")
	if got := e.rows(40); got != minInputRows {
		t.Errorf("rows(one short line) = %d, want %d", got, minInputRows)
	}

	e.input.SetValue("a\nb\nc")
	if got := e.rows(40); got != 3 {
		t.Errorf("rows(three lines) = %d, want 3", got)
	}

	e.input.SetValue(strings.Repeat("line\n", maxInputRows*3))
	if got := e.rows(40); got != maxInputRows {
		t.Errorf("rows(overflow) = %d, want the %d cap", got, maxInputRows)
	}
}
