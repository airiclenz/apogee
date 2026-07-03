package tui

import (
	"charm.land/bubbles/v2/textarea"
)

// ----------------------------------------------------------------------------
// promptEditor — the chat input cluster lifted off the god-Model (review candidate #3)
// ----------------------------------------------------------------------------
//
// promptEditor gathers the five loose input-side concerns the architecture review called one
// coherent concept: the textarea, the autocomplete overlay (+ its skillRegion edge-trigger), the
// staged-skill chips, the workspace file cache, and the prompt drag-selection. The Model embeds it
// ANONYMOUSLY (model.go), so its fields and its self-contained methods promote onto the Model —
// m.input, m.pendingSkills, m.caretTo(...) all still resolve — which keeps the value-copied Model
// idiom and every existing call site (and its tests) unchanged while the input state gains a home.
//
// The lift is deliberately PARTIAL (design decision Option C): only methods that touch nothing but
// the editor's own fields live here. Methods that also read Model state the editor does not own —
// the theme, the window width/height, the display Options, the lifecycle state — stay on the Model
// rather than duplicate that state here (computeAutocomplete, acceptAutocomplete, attachSkill,
// highlightInput, inputContentRect, the region-arbitrating mouse handlers). No Model state is
// copied onto the editor, and the editor never touches the engine — it only turns what the human
// typed into send-ingredients the Model then routes.

// promptEditor owns the chat input cluster. The zero value is not usable — build one with
// newPromptEditor, which focuses the textarea and installs the file cache.
type promptEditor struct {
	// input is the message textarea (Bubbles widget), the black-interior auto-growing field.
	input textarea.Model

	// autocomplete is the chat mini-language suggestion overlay shown while typing at idle
	// (commands on "/", workspace files on "@", skills on "/skill"). The zero value is hidden.
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

	// pendingSkills are the skill IDs attached via the /skill picker, awaiting the next submit
	// (which copies them into UserInput.SkillIDs and clears them). A plain []string — a
	// reference header, safe in the value-copied Model (ADR 0011) — rendered as chips above the
	// input. Backspace on an empty input pops the last one.
	pendingSkills []string

	// sel is the prompt's mouse drag-selection (mouse.go); the zero value is "no selection". It
	// is cleared by any keypress, a submit/reset, or a resize, so its visual coords never go
	// stale. It and the Model's transcriptSel never coexist (region arbitration in the mouse
	// handlers).
	sel promptSel
}

// newPromptEditor builds the idle input cluster: a focused, black-interior, auto-growing textarea
// (its newline binding repurposed because plain Enter submits) and an empty workspace file cache.
// The Focus Cmd is discarded here — the focus STATE is what matters at construction; the cursor's
// blink Cmd is returned later by Model.Init.
func newPromptEditor() promptEditor {
	ta := textarea.New()
	ta.Placeholder = "Send a message…  ⏎ send · ⇧⏎/⌥⏎ newline · ⌃c quit"
	ta.Prompt = "" // the rounded border is the frame; no inline prompt gutter (layout.md)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // no limit; the model, not the widget, bounds a turn
	// Plain Enter submits (intercepted in handleKey), so the textarea's newline binding is
	// repurposed: shift+enter works on terminals that support the Kitty keyboard protocol,
	// and alt+enter / ctrl+j are byte-distinct fallbacks that insert a newline everywhere.
	ta.KeyMap.InsertNewline.SetKeys("shift+enter", "alt+enter", "ctrl+j")
	blackenInput(&ta)
	ta.Focus()
	return promptEditor{input: ta, files: &fileCache{}}
}

// submitParse parses the editor's current input through the chat mini-language (command.go) and
// returns the parse together with the skill IDs staged for attachment. It reads the editor
// without mutating it: the caller resets the editor only once it has decided the parse is a
// message to send (a recognised /command routes without a reset). This is the editor's "turn what
// I hold into send-ingredients" seam — unit-testable without a Model or a fake engine.
func (e promptEditor) submitParse() (parsedInput, []string) {
	return parseInput(e.input.Value()), e.pendingSkills
}

// reset clears the editor back to empty after a message is sent: it empties the textarea, closes
// the autocomplete overlay, and drops the staged-skill chips. The prompt drag-selection is
// already gone by here — the keypress that reached submit cleared it (handleKey).
func (e *promptEditor) reset() {
	e.input.Reset()
	e.autocomplete = autocompleteState{}
	e.pendingSkills = nil
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
