package tui

import (
	"reflect"
	"strings"
	"testing"
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
	e := newPromptEditor(defaultCursorShape)
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
	e := newPromptEditor(defaultCursorShape)
	e.input.SetValue("/clear")
	parsed := e.submitParse(nil)
	if parsed.kind != kindCommand || parsed.command != "clear" {
		t.Fatalf("parsed = %+v, want kindCommand verb=clear", parsed)
	}
}

// submitParse resolves the inline /tokens through the predicate it is handed, so a message that
// names a skill arrives with the id extracted and the token still in its text.
func TestPromptEditorSubmitParseExtractsSkillTokens(t *testing.T) {
	e := newPromptEditor(defaultCursorShape)
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
	e := newPromptEditor(defaultCursorShape)
	e.input.SetValue("half-typed /skill go")
	e.autocomplete = autocompleteState{active: true, kind: acSkill}
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

// rows grows one row per logical line and clamps at maxInputRows.
func TestPromptEditorRowsGrowsAndClamps(t *testing.T) {
	e := newPromptEditor(defaultCursorShape)

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
