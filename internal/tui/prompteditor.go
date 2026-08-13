package tui

import (
	"fmt"
	"image/color"
	"strings"

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
// The FIELD half of it — the textarea and the caret arithmetic over it — has since moved down one
// level into [lineEditor] (lineeditor.go), which this type embeds and the /settings value row builds
// its own of: a text field is not a chat prompt, and the pane must not inherit this one's vocabulary
// (recall, submit, the slash menu). Promotion means nothing above here moved.
//
// The lift is deliberately PARTIAL (design decision Option C): only methods that touch nothing but
// the editor's own fields live here. Methods that also read Model state the editor does not own —
// the theme, the window width/height, the display Options, the lifecycle state — stay on the Model
// rather than duplicate that state here (computeAutocomplete, acceptAutocomplete,
// insertSkillToken, highlightInput, accentTokens, inputContentRect, the region-arbitrating mouse
// handlers). No
// Model state is copied onto the editor, and the editor never touches the engine — it only turns
// what the human typed into send-ingredients the Model then routes.

// promptEditor owns the chat input cluster. The zero value is not usable — build one with
// newPromptEditor, which focuses the textarea and installs the file cache.
type promptEditor struct {
	// lineEditor is the field itself — the textarea and the caret arithmetic over it (lineeditor.go),
	// shared with the /settings value row. It is EMBEDDED so its field and methods promote onto the
	// promptEditor and on through the Model (m.input, m.caretTo(...) still resolve), which is the same
	// posture the Model embeds this type in: the chat box is a text field with the chat machinery
	// below around it.
	lineEditor

	// autocomplete is the chat mini-language suggestion overlay shown while typing: one merged menu
	// of commands and skills on a "/" token, and workspace files on "@". Every region follows the
	// TOKEN AT THE CARET and opens wherever the box is editable, interjections included. The zero
	// value is hidden.
	autocomplete autocompleteState

	// skillRegion tracks whether the input currently sits in a "/<partial>" menu region, so
	// recomputeAutocomplete can edge-trigger a catalog reload only when the menu OPENS (the
	// false→true transition) rather than on every keystroke inside it. It follows the region
	// itself, not autocomplete.active, so a region that momentarily shows no matches still
	// counts as open and does not re-reload on the next matching keystroke.
	//
	// It is also what says whether a finished re-scan is owed a repaint (foldSkillsReloaded), which
	// is why dismissAutocomplete clears it: a menu esc dismissed, or one a modal took the frame from,
	// is not a region a walk landing afterwards may paint back.
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

	// recall is the prompt-recall state (recall.go): this workspace's recorded inputs, loaded once
	// at start-up from [Options.Recall]. The zero value is "nothing to recall", which is where an
	// unwired host leaves it — a plain value with a replaced-never-mutated slice, so it copies with
	// the Model like sel above (ADR 0011).
	recall promptRecall

	// keyDisambiguation records whether the terminal negotiated the enhanced (kitty) keyboard
	// protocol's key disambiguation — the thing that makes ⇧⏎ reach the program as a key of its
	// own instead of as a plain ⏎. It starts FALSE and is set from tea.KeyboardEnhancementsMsg
	// (the arm in model.go), which capable terminals answer within the first frames; on a terminal
	// that never answers it stays false, which is the honest reading rather than a guess.
	//
	// Only the idle legend reads it (idleLegend). The textarea's InsertNewline binding keeps
	// "shift+enter" either way — a chord the terminal never delivers costs nothing to leave bound,
	// and unbinding it would break the terminals that do.
	keyDisambiguation bool
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
// Both legends advertise ↑ recall, and both earn it: the placeholder is drawn only on an EMPTY box,
// which is exactly the box where ↑ starts a walk through what this workspace has sent (recall.go),
// and the walk is live at idle and while a worker runs alike.
//
// The idle one comes in two forms because one of the keys it names is not always there: ⇧⏎ reaches
// the program only on a terminal that negotiated the enhanced keyboard protocol, and everywhere
// else the terminal folds it into a plain ⏎ — which is a SEND. Advertising it unconditionally
// therefore promises a newline and delivers a sent message, so the chord is named only once the
// answer to bubbletea's keyboard query says it will arrive (idleLegend).
const (
	idlePlaceholder      = "Send a message…  ⏎ send · ⌥⏎ newline · ↑ recall · ⌃c quit"
	idleShiftPlaceholder = "Send a message…  ⏎ send · ⇧⏎/⌥⏎ newline · ↑ recall · ⌃c quit"
	runningPlaceholder   = "queue a message…  ⏎ queue · ↑ recall · esc stop"
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

// newPromptEditor builds the idle input cluster: the shared text field (newLineEditor — focused,
// filled with the active scheme's `surface` tone, the terminal's own steady caret in the given
// shape), given the chat box's own two differences from every other field in this package, and an
// empty workspace file cache.
func newPromptEditor(shape tea.CursorShape, surface color.Color) promptEditor {
	e := newLineEditor(shape, surface)
	e.input.Placeholder = idlePlaceholder // the not-yet-negotiated legend; idleLegend upgrades it if the terminal answers
	// Plain Enter submits (intercepted in handleKey), so the textarea's newline binding is
	// repurposed: shift+enter works on terminals that support the Kitty keyboard protocol,
	// and alt+enter / ctrl+j are byte-distinct fallbacks that insert a newline everywhere.
	e.input.KeyMap.InsertNewline.SetKeys("shift+enter", "alt+enter", "ctrl+j")
	return promptEditor{lineEditor: e, files: &fileCache{}}
}

// submitParse parses the editor's current input through the chat mini-language (command.go): the
// verb or the message text, its @file references, and its inline "/id" skill references. It reads the
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

// setPlaceholder swaps what the empty box invites (idleLegend / runningPlaceholder). It is
// called on the lifecycle transitions that open and close an Exchange, not per frame, so the
// legend is one assignment rather than a branch in the renderer.
func (e *promptEditor) setPlaceholder(text string) {
	e.input.Placeholder = text
}

// idleLegend is the idle invitation as it stands for THIS terminal: the ⇧⏎ form once key
// disambiguation was negotiated, the ⌥⏎-only form until then. Callers hand its result to
// setPlaceholder rather than naming a constant, so there is one place that decides which chords
// the box may claim.
//
// The default is the pessimistic one on purpose: a terminal that supports the protocol confirms it
// within the first frames and the legend catches up (setKeyDisambiguation), while one that never
// will keeps a legend naming only keys it actually delivers. ⌥⏎ is byte-distinct and works
// everywhere; ctrl+j remains a working, undocumented third fallback.
func (e promptEditor) idleLegend() string {
	if e.keyDisambiguation {
		return idleShiftPlaceholder
	}
	return idlePlaceholder
}

// setKeyDisambiguation records the terminal's answer to the keyboard-protocol query and re-resolves
// the legend in place when the box is currently showing an idle one — the answer lands a few frames
// after start-up, by which time the idle placeholder has already been set from the pessimistic
// default, and nothing else would revisit it until the next lifecycle transition. A box showing the
// running invitation is left alone: that legend names no newline chord, and the transition back to
// idle re-resolves it anyway.
func (e *promptEditor) setKeyDisambiguation(ok bool) {
	e.keyDisambiguation = ok
	if e.input.Placeholder == idlePlaceholder || e.input.Placeholder == idleShiftPlaceholder {
		e.input.Placeholder = e.idleLegend()
	}
}

// reset clears the editor back to empty after a message is sent: it empties the textarea, closes
// the autocomplete overlay and clears the skillRegion edge-trigger with it. Emptying the textarea
// is all it takes to drop the skills too — they are /tokens IN the text, not state beside it. The
// prompt drag-selection is already gone by here — the keypress that reached submit cleared it
// (handleKey).
//
// The edge-trigger is cleared for dismissAutocomplete's reason: an emptied box sits in no menu
// region, so leaving it true would make the NEXT "/" typed a non-opening — recomputeAutocomplete
// would skip its re-scan and the menu would list the catalog as it stood before the submit. Submit
// on an exact "/skill" token is the reachable case: the region is open at the moment ⏎ lands.
func (e *promptEditor) reset() {
	e.input.Reset()
	e.autocomplete = autocompleteState{}
	e.skillRegion = false
}

// rows reports the textarea's height in visual rows for a given text width: the rows its current
// content wraps to (inputContentRows mirrors the widget's own wrap), clamped to
// [minInputRows, maxInputRows]. The box grows as the human types a multi-line message and stops
// growing at the cap, where the textarea scrolls internally. innerWidth is a Model concern (it
// derives from the window), so the Model passes it in rather than the editor duplicating it.
func (e promptEditor) rows(innerWidth int) int {
	return clampInt(inputContentRows(e.input.Value(), innerWidth), minInputRows, maxInputRows)
}
