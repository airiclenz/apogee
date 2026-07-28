package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// promptEditor — the chat input cluster lifted off the god-Model (review candidate #3)
// ----------------------------------------------------------------------------
//
// promptEditor gathers the loose input-side concerns the architecture review called one coherent
// concept: the textarea, the autocomplete overlay (+ its skillRegion edge-trigger), the workspace
// file cache, and the prompt drag-selection. The Model embeds it ANONYMOUSLY (model.go), so its
// fields and its self-contained methods promote onto the Model — m.input, m.autocomplete,
// m.caretTo(...) all still resolve — which keeps the value-copied Model idiom and every existing
// call site (and its tests) unchanged while the input state gains a home.
//
// The lift is deliberately PARTIAL (design decision Option C): only methods that touch nothing but
// the editor's own fields live here. Methods that also read Model state the editor does not own —
// the theme, the window width/height, the display Options, the lifecycle state — stay on the Model
// rather than duplicate that state here (computeAutocomplete, acceptAutocomplete,
// insertSkillToken, highlightInput, inputContentRect, the region-arbitrating mouse handlers). No
// Model state is copied onto the editor, and the editor never touches the engine — it only turns
// what the human typed into send-ingredients the Model then routes.

// promptEditor owns the chat input cluster. The zero value is not usable — build one with
// newPromptEditor, which focuses the textarea and installs the file cache.
type promptEditor struct {
	// input is the message textarea (Bubbles widget), the black-interior auto-growing field.
	input textarea.Model

	// autocomplete is the chat mini-language suggestion overlay shown while typing (commands on
	// "/", workspace files on "@", skills on "/skill" — the first and last at idle only, the file
	// region wherever the box is editable, interjections included). The zero value is hidden.
	autocomplete autocompleteState

	// skillRegion tracks whether the input currently sits in a "/skill <partial>" region, so
	// recomputeAutocomplete can edge-trigger a catalog reload only when the picker OPENS (the
	// false→true transition) rather than on every keystroke inside it. It follows the region
	// itself, not autocomplete.active, so a region that momentarily shows no matches still
	// counts as open and does not re-reload on the next matching keystroke.
	skillRegion bool

	// files memoises the workspace listing behind the "@" autocomplete so a typing burst reuses
	// one filesystem walk (filecache.go). A pointer — shared across the value-copied Model so
	// the cache survives each Update (ADR 0011); nil-safe (fileSuggestions falls back).
	files *fileCache

	// sel is the prompt's mouse drag-selection (mouse.go); the zero value is "no selection". It
	// is cleared by any keypress, a submit/reset, or a resize, so its visual coords never go
	// stale. It and the Model's transcriptSel never coexist (region arbitration in the mouse
	// handlers).
	sel promptSel
}

// The prompt's two placeholders — what the empty box invites, which is not the same thing while a
// worker runs. At idle (and while answering an ask_user question) a message is SENT; while the
// model works it is queued as an interjection and delivered at the next boundary (ADR 0025), so
// the legend says what ⏎ will actually do and keeps esc's meaning visible.
//
// They are swapped on the lifecycle transitions that open and close an Exchange (setPlaceholder,
// called from the launch paths and finishWorker), never derived per frame: View renders the
// widget as it stands, so the placeholder is state the Model sets once rather than a render-time
// branch.
const (
	idlePlaceholder    = "Send a message…  ⏎ send · ⇧⏎/⌥⏎ newline · ⌃c quit"
	runningPlaceholder = "queue a message…  ⏎ queue · esc stop"
)

// cursorShapeNames is the vocabulary [ParseCursorShape] accepts, in the order its error lists
// them, paired with the renderer constant each name means. Declared once so no caller re-types
// the set — the spinnerStyleNames posture.
var cursorShapeNames = []struct {
	name  string
	shape tea.CursorShape
}{
	{"block", tea.CursorBlock},
	{"underline", tea.CursorUnderline},
	{"bar", tea.CursorBar},
}

// defaultCursorShape is what an unset config value resolves to.
const defaultCursorShape = tea.CursorBlock

// ParseCursorShape maps a config value onto the shape the prompt's caret is drawn with. "" ⇒ the
// default (block); an unknown value is an error naming the shapes. The caller names the key it read
// the value from — this package does not know the config schema (as with [ParseSpinnerStyle], the
// vocabulary lives here so the config layer validates against one source of truth).
//
// The set is closed at three because that is what a terminal cursor can be. Inheriting the shape
// the terminal itself is configured with is deliberately NOT among them: a Bubble Tea program names
// a shape on every frame and never emits the DECSCUSR reset while it runs, so there is nothing to
// inherit back into — this key is the honest substitute (the terminal's own cursor returns on exit).
func ParseCursorShape(s string) (tea.CursorShape, error) {
	if s == "" {
		return defaultCursorShape, nil
	}
	for _, c := range cursorShapeNames {
		if s == c.name {
			return c.shape, nil
		}
	}
	names := make([]string, 0, len(cursorShapeNames))
	for _, c := range cursorShapeNames {
		names = append(names, c.name)
	}
	return defaultCursorShape, fmt.Errorf("unknown cursor shape %q (known shapes: %s)", s, strings.Join(names, ", "))
}

// newPromptEditor builds the idle input cluster: a focused, black-interior, auto-growing textarea
// (its newline binding repurposed because plain Enter submits) and an empty workspace file cache.
// shape is the caret's shape: the widget's simulated cursor is retired here (steadyCursor) and
// [Model.View] hands Bubble Tea the REAL terminal cursor at the caret instead, steady in that
// shape. The Focus Cmd is discarded — the focus STATE is what matters at construction, and a
// retired virtual cursor has no blink to schedule, so that Cmd is nil now anyway.
func newPromptEditor(shape tea.CursorShape) promptEditor {
	ta := textarea.New()
	ta.Placeholder = idlePlaceholder
	ta.Prompt = "" // the rounded border is the frame; no inline prompt gutter (layout.md)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // no limit; the model, not the widget, bounds a turn
	// Plain Enter submits (intercepted in handleKey), so the textarea's newline binding is
	// repurposed: shift+enter works on terminals that support the Kitty keyboard protocol,
	// and alt+enter / ctrl+j are byte-distinct fallbacks that insert a newline everywhere.
	ta.KeyMap.InsertNewline.SetKeys("shift+enter", "alt+enter", "ctrl+j")
	blackenInput(&ta)
	steadyCursor(&ta, shape)
	ta.Focus()
	return promptEditor{input: ta, files: &fileCache{}}
}

// submitParse parses the editor's current input through the chat mini-language (command.go): the
// verb or the message text, its @file references, and its inline /skill references. It reads the
// editor without mutating it: the caller resets the editor only once it has decided the parse is a
// message to send (a recognised /command routes without a reset). This is the editor's "turn what
// I hold into send-ingredients" seam — unit-testable without a Model or a fake engine.
//
// known is the catalog predicate parseInput resolves skill tokens against. The editor does not own
// the catalog (it owns nothing but the box — the partial-lift rule above), so the Model passes its
// own [Model.knownSkillID] in rather than this type growing a copy of Model state.
func (e promptEditor) submitParse(known func(string) bool) parsedInput {
	return parseInput(e.input.Value(), known)
}

// setPlaceholder swaps what the empty box invites (idlePlaceholder / runningPlaceholder). It is
// called on the lifecycle transitions that open and close an Exchange, not per frame, so the
// legend is one assignment rather than a branch in the renderer.
func (e *promptEditor) setPlaceholder(text string) {
	e.input.Placeholder = text
}

// reset clears the editor back to empty after a message is sent: it empties the textarea and
// closes the autocomplete overlay. Emptying the textarea is all it takes to drop the skills too —
// they are /tokens IN the text, not state beside it. The prompt drag-selection is already gone by
// here — the keypress that reached submit cleared it (handleKey).
func (e *promptEditor) reset() {
	e.input.Reset()
	e.autocomplete = autocompleteState{}
}

// rows reports the textarea's height in visual rows for a given text width: the rows its current
// content wraps to (inputContentRows mirrors the widget's own wrap), clamped to
// [minInputRows, maxInputRows]. The box grows as the human types a multi-line message and stops
// growing at the cap, where the textarea scrolls internally. innerWidth is a Model concern (it
// derives from the window), so the Model passes it in rather than the editor duplicating it.
func (e promptEditor) rows(innerWidth int) int {
	return clampInt(inputContentRows(e.input.Value(), innerWidth), minInputRows, maxInputRows)
}

// reseatCaret drives the textarea caret to an absolute visual (soft-wrapped) row through the
// widget's own primitives: MoveToBegin resets to the top — which "unscrolls" its internal
// viewport to offset 0 — and each CursorDown steps down one visual row, clamping at the end.
// Walking down from the top re-clamps a scroll offset the widget left stale (its SetHeight only
// repositions when the caret falls outside the view, never when the box grows), so the caret
// lands on its real visual row with the least scroll that keeps it visible. It re-derives none
// of the textarea's wrap, so the geometry holds across bubbles releases. Shared by caretTo (a
// mouse click's target row) and reseatInput (the caret's own row, after a height change).
func (e *promptEditor) reseatCaret(visRow int) {
	e.input.MoveToBegin()
	for i := 0; i < visRow; i++ {
		e.input.CursorDown()
	}
}

// caretTo positions the textarea caret at the given absolute visual cell and returns the
// caret's rune offset into the value. It re-seats to the target visual row through reseatCaret
// (the widget's own wrap-aware walk), then LineInfo locates the landed visual line — so the
// result matches what the textarea actually draws without re-deriving its wrap.
func (e *promptEditor) caretTo(visRow, visCol int) int {
	e.reseatCaret(visRow)
	li := e.input.LineInfo()
	// visCol is a display-cell offset from the row's start, but SetCursorColumn indexes runes
	// into the logical line — the two diverge on any CJK/emoji row. Walk the landed visual
	// sub-line's runes, accumulating display width, to convert the cell column to a rune offset;
	// StartColumn (a rune offset) then anchors it back into the logical line. Feeding the raw
	// cell column would drop the caret on the wrong rune, and a drag-copy would then put
	// different text on the clipboard than the highlight showed.
	sub := visualSubline(e.input.Value(), e.input.Line(), li.StartColumn, li.Width)
	e.input.SetCursorColumn(li.StartColumn + cellToRuneOffset(sub, visCol))
	return caretOffset(e.input.Value(), e.input.Line(), e.input.Column())
}

// reseatInput re-clamps the prompt textarea's internal scroll after a SetHeight changed the
// box's height. bubbles repositions the view only when the caret falls outside it, so a box that
// auto-grows keeps a stale downward offset — the first content line scrolls out of sight with a
// phantom blank row below (ISSUES #2). Re-seating the caret onto its own visual row through the
// shared reseatCaret idiom unscrolls to the top and re-clamps the offset to the current height,
// leaving the caret exactly where it was. The caret's visual row is the wrapped rows above its
// logical line — counted here with the widget's own CursorDown so no wrap is re-derived — plus
// its within-line sub-row; the logical column is captured and restored so the caret does not
// move. layout() calls this only on a height change, which never happens during vertical caret
// navigation, so the textarea's remembered goal column is untouched.
func (e *promptEditor) reseatInput() {
	row, col := e.input.Line(), e.input.Column()
	visRow := e.input.LineInfo().RowOffset // the caret's sub-row within its logical line
	e.input.MoveToBegin()
	for e.input.Line() < row { // count the wrapped rows of the logical lines above the caret
		e.input.CursorDown()
		visRow++
	}
	e.reseatCaret(visRow)
	e.input.SetCursorColumn(col)
}
