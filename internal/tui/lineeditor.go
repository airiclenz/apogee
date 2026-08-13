package tui

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// lineEditor — the editable text field both boxes in this package are built from
// ----------------------------------------------------------------------------
//
// The chat prompt was this package's only text field for as long as there was only one thing to
// type into. The /settings pane made that untrue: a value row is a field too, and it needs the same
// caret — ←/→ and the word jumps, home/end, insertion and deletion in the MIDDLE of what is there —
// without inheriting one word of the prompt's own vocabulary (recall, submit, the slash menu, the
// "@" file overlay). So the field itself is the type, and the prompt is a field with the chat
// machinery around it: [promptEditor] EMBEDS this, and the settings pane builds one of its own.
//
// The alternative — the pane holding a configured promptEditor — was considered and rejected (plan
// item 10, owner's call 2026-08-06): a settings row must not depend on prompt internals, and the
// items built on this seam (mouse caret seating, string lists, the multiline system-prompt editor)
// would each have had to reason about which prompt behaviours were switched off.

// lineEditor is a text FIELD and nothing else: a bubbles textarea plus the caret arithmetic over it.
// The caret family is the substance — a mouse click names a VISUAL cell (caretTo, through
// reseatCaret), a splice names a LOGICAL position or a byte offset (seatCaret, caretToOffset) — and
// it is written once here so the prompt and the settings value row cannot drift apart on where the
// caret goes.
//
// It is held BY VALUE by both its callers, as the Model has always held the prompt's (ADR 0011,
// doc.go): the widget carries no self-referential no-copy type, and what it holds by pointer (its
// viewport, its wrap cache) is shared across the Model's copies deliberately, which is the posture
// the chat box has run in since it was built.
//
// It knows nothing about what is being edited: what a value may hold, what commits it and what
// abandons it all belong to the caller. That is the whole boundary — this type turns keystrokes into
// text and says where the caret stands.
type lineEditor struct {
	// input is the widget: the solid-interior field whose simulated cursor is retired in favour of
	// the terminal's own (steadyCursor).
	input textarea.Model

	// oneLine records the confinement [lineEditor.singleLine] applied, because text can still arrive
	// carrying a newline: a paste is not a keystroke, so the newline BINDING being switched off does
	// not cover it. The field keeps its own invariant rather than asking every caller to remember it
	// (lineEditor.editMsg → flattenLine).
	oneLine bool
}

// newLineEditor builds the part every text field in this package shares: a focused, solid-interior
// textarea with no prompt gutter, no line numbers, no character limit of its own, and the terminal's
// real caret in the given shape. What the callers add differs — [newPromptEditor] adds the chat
// placeholder and a newline binding, [lineEditor.singleLine] takes the vertical dimension away.
//
// surface is the active scheme's field tone ([theme.surface]) the widget's four background slots are
// painted with (fillInput): the textarea is Bubble Tea's, so the colour has to be handed to it
// rather than looked up.
//
// The Focus Cmd is discarded: the focus STATE is what matters at construction, and a retired virtual
// cursor has no blink to schedule, so that Cmd is nil anyway.
func newLineEditor(shape tea.CursorShape, surface color.Color) lineEditor {
	ta := textarea.New()
	ta.Prompt = "" // the caller's own frame is the field's border; no inline prompt gutter (layout.md)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // no limit; what a value may hold is the caller's business, not the widget's
	fillInput(&ta, surface)
	steadyCursor(&ta, shape)
	ta.Focus()
	return lineEditor{input: ta}
}

// singleLine confines the editor to ONE line, which is two things at once. The widget's newline
// binding is switched off, so no key it knows can split the value — ⏎ is then free to mean whatever
// the surface above says it means, which for the settings pane is "commit" — and its vertical
// navigation goes with it: a one-line field has no line to move up or down TO, and those bindings
// would otherwise walk the caret across the visual rows a long value wraps to, which is not
// something the single row the field is drawn on can show.
//
// The widget's own ctrl+v STAYS, and what it needs is not a binding but a route: the clipboard read
// it starts comes back as a Msg of the widget package's own unexported type, so it is delivered by
// whichever surface owns the keyboard rather than by the type of the Msg ([Model.Update]'s default
// arm, through Model.settingsEditorMsg). What such a paste may carry that no keystroke can is a
// NEWLINE, which is why the confinement is recorded (oneLine) and re-imposed on the text that
// arrives (lineEditor.editMsg).
func (e *lineEditor) singleLine() {
	e.input.SetHeight(1)
	e.oneLine = true
	for _, b := range []*key.Binding{
		&e.input.KeyMap.InsertNewline,
		&e.input.KeyMap.LineNext,
		&e.input.KeyMap.LinePrevious,
		&e.input.KeyMap.PageUp,
		&e.input.KeyMap.PageDown,
	} {
		b.SetEnabled(false)
	}
}

// value is what the field currently holds.
func (e lineEditor) value() string {
	return e.input.Value()
}

// setValue replaces the whole value and leaves the caret at the END of it — where a human correcting
// what a field already holds starts from, and the one seat a freshly opened field can be sure is
// inside its text.
func (e *lineEditor) setValue(s string) {
	e.input.SetValue(s)
	e.input.MoveToEnd()
}

// editKey hands one keypress to the widget and returns whatever Cmd it asks for. This is the whole
// of what "typing in this field" means: insertion at the caret, backspace and delete around it, ←/→
// and the word jumps, home/end — the widget's own key map, which is exactly what the chat box gives
// and therefore exactly what a value row should.
//
// The caller routes the keys that are ITS OWN — a commit, a cancel — before reaching here, so the
// field never has to know what ends an edit.
func (e *lineEditor) editKey(msg tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	e.input, cmd = e.input.Update(msg)
	return cmd
}

// editMsg hands one NON-key Msg to the field and returns whatever Cmd it asks for — editKey's
// counterpart for the two messages that are text rather than keystrokes: the terminal's bracketed
// paste (tea.PasteMsg) and the clipboard reply the widget's own ctrl+v asked for, whose type is the
// widget package's own and unexported. Neither can be recognised by a surface that switches on Msg
// type, so the ROUTE is the surface that owns the keyboard (Model.settingsEditorMsg), and this is
// where such a Msg enters the field.
//
// A single-line field is flattened afterwards because this is the one door a newline can come
// through: the newline BINDING is off (lineEditor.singleLine) but pasted text carries its own line
// breaks, and a value holding one would break the single row it is painted in (settingBufferCells).
func (e *lineEditor) editMsg(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	e.input, cmd = e.input.Update(msg)
	if e.oneLine {
		e.flattenLine()
	}
	return cmd
}

// flattenLine folds a multi-line value onto one line — each newline, each tab and each carriage
// return becoming the space that stands where it was — and leaves the caret on the same rune it
// stood on: the substitution is one rune for one rune, so every offset into the value still names
// what it named. It is what keeps a field built single-line single-line when text arrives from
// somewhere other than the keyboard (lineEditor.editMsg).
//
// All three, not the newline alone: the door this guards is a bracketed paste, and pasted text
// carries whatever the clipboard held — a tab from a table cell, a CRLF from a Windows terminal —
// none of which a single-line binding can refuse. A tab in particular is the newline's forgery
// sideways: lipgloss counts it as one cell while the terminal expands it to the next tab stop, so a
// value carrying one is laid out at one width and drawn at another (the display-seam sibling
// [flattenField] folds it for that same reason).
//
// Today the widget's own rune sanitizer gets there first — every write into a bubbles textarea runs
// through it, and it maps a tab to four spaces and a carriage return to a newline before this sees
// the value — so the two widened cases are the field's OWN invariant rather than one borrowed from a
// dependency's default configuration, which is the point: what a one-line field may hold is decided
// here, and stays decided if that sanitizer is ever configured differently or replaced. The fold
// stays one rune for one rune, which is what the caret arithmetic below rests on.
//
// A space rather than nothing at all: the two lines were separate words, and a commit trims what a
// trailing newline leaves behind (settingsCommitBuffer's TrimSpace), so a path copied from a terminal
// with its line ending still on it pastes as the path.
func (e *lineEditor) flattenLine() {
	value := e.input.Value()
	if !strings.ContainsAny(value, "\n\t\r") {
		return
	}
	off := e.caretRune()
	e.input.SetValue(lineBreaks.Replace(value))
	e.caretToRune(off)
}

// lineBreaks is flattenLine's substitution, built once: each character it folds is a single byte in
// and a single byte out, so the replacer walks the value in one pass and leaves every other byte —
// an invalid one included — exactly as it found it.
var lineBreaks = strings.NewReplacer("\n", " ", "\t", " ", "\r", " ")

// stepLine walks the caret one LOGICAL line up or down, keeping the column it stands in and clamping
// at the value's first and last lines. It is the step a surface that paints one line per logical line
// scrolls by (the /settings multi-line field under the wheel, mouse.go): the widget's own CursorUp and
// CursorDown walk the VISUAL rows of its own wrap, which is not what such a surface drew.
func (e *lineEditor) stepLine(delta int) {
	value := e.input.Value()
	row := clampInt(e.input.Line()+delta, 0, strings.Count(value, "\n"))
	e.seatCaret(row, e.input.Column())
}

// caretRune is where the caret stands as a RUNE offset into the value — the widget's own (row,
// column) flattened, which is the coordinate every caller that slices the value counts in.
func (e lineEditor) caretRune() int {
	return caretOffset(e.input.Value(), e.input.Line(), e.input.Column())
}

// caretLine is the LOGICAL line the caret stands on — which line of a multi-line value the next
// keystroke lands in. A surface that paints the value one line per line reads it to know which of
// those lines to keep on the screen (renderSettingsText); the chat box has no use for it, because the
// widget draws and scrolls itself there.
func (e lineEditor) caretLine() int {
	return e.input.Line()
}

// textWithCaret is the field as PLAIN TEXT with a caret glyph drawn into it at the caret's position.
// It is what a surface that takes no styling of its own paints the field with: the popup module
// styles rows whole and its cells must arrive as plain, escape-free text (popup.go, doc.go), so a
// field drawn inside one cannot hand it the widget's own View — and the terminal's real cursor has
// no seat on a popup row to be placed at either. A glyph AT the offset is then the honest report of
// where the next keystroke lands, which is what the caret is for.
func (e lineEditor) textWithCaret(caret string) string {
	r := []rune(e.input.Value())
	off := clampInt(e.caretRune(), 0, len(r))
	return string(r[:off]) + caret + string(r[off:])
}

// reseatCaret drives the textarea caret to an absolute visual (soft-wrapped) row through the
// widget's own primitives: MoveToBegin resets to the top — which "unscrolls" its internal
// viewport to offset 0 — and each CursorDown steps down one visual row, clamping at the end.
// Walking down from the top re-clamps a scroll offset the widget left stale (its SetHeight only
// repositions when the caret falls outside the view, never when the box grows), so the caret
// lands on its real visual row with the least scroll that keeps it visible. It re-derives none
// of the textarea's wrap, so the geometry holds across bubbles releases. It serves caretTo — a
// mouse click names a VISUAL row, which is the only thing this walk can express; a caret named by
// a LOGICAL row and column is seated by seatCaret instead, whose step cannot stall.
func (e *lineEditor) reseatCaret(visRow int) {
	e.input.MoveToBegin()
	for i := 0; i < visRow; i++ {
		e.input.CursorDown()
	}
}

// caretTo positions the textarea caret at the given absolute visual cell and returns the
// caret's rune offset into the value. It re-seats to the target visual row through reseatCaret
// (the widget's own wrap-aware walk), then LineInfo locates the landed visual line — so the
// result matches what the textarea actually draws without re-deriving its wrap.
func (e *lineEditor) caretTo(visRow, visCol int) int {
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

// caretByteOffset reports where the caret stands as a BYTE offset into the editor's value. The
// widget positions its cursor by logical row and RUNE column while the chat mini-language scans the
// value by byte (command.go, autocomplete.go), so every completion region reads the caret through
// this one conversion instead of each doing its own — and every one of them is then a pure function
// of (value, offset), unit-testable without driving a widget.
func (e lineEditor) caretByteOffset() int {
	return byteOffsetOf(e.input.Value(), e.caretRune())
}

// seatCaret drives the textarea caret to a LOGICAL (row, column) and re-clamps the widget's
// internal scroll onto it — the one seat both the completion splice (caretToOffset) and the
// auto-grow re-clamp (reseatInput) are expressed in. Like reseatCaret it re-derives none of the
// textarea's wrap; unlike reseatCaret it steps LOGICAL lines, which is what makes it total.
//
// The step is Height-aware, and that is the whole point. bubbles' CursorDown leaves a logical line
// only when the caret already sits on that line's LAST wrapped sub-row (RowOffset+1 >= Height);
// anywhere above it, the step guesses the next sub-row's column as min(StartColumn+Width+2,
// len(line)-1). A logical line that ends with a space exactly at a row boundary wraps to a PHANTOM
// trailing sub-line (bubbles' wrap appends one), and that len(line)-1 clamp can never reach it — so
// a walk of bare CursorDowns stands still forever on such a line: it neither crosses it nor moves at
// all. CursorEnd first puts the caret at the end of the logical line, which IS the last sub-row
// (phantom included) at every width, so the following CursorDown always lands on the next logical
// line. Each pass therefore advances Line() by exactly one and the walk cannot stall or spin; the
// break is unreachable defence, since offsetToLineCol clamps row to a line the value has.
//
// MoveToBegin unscrolls to offset 0 first, so the walk down re-clamps the offset with the least
// scroll that keeps the caret visible (bubbles repositions only when the caret falls OUTSIDE the
// view, so a box that just grew keeps a stale downward offset — ISSUES #2). SetCursorColumn does
// not reposition, so the final SetHeight — at the height the box already has, hence a no-op to the
// geometry — re-runs the widget's own repositioning on the seated caret without moving it, which is
// what keeps a caret deep inside a wrapped line on screen.
func (e *lineEditor) seatCaret(row, col int) {
	e.input.MoveToBegin()
	for e.input.Line() < row {
		before := e.input.Line()
		e.input.CursorEnd()  // the logical line's last wrapped sub-row, phantom included
		e.input.CursorDown() // ⇒ the next logical line
		if e.input.Line() == before {
			break // unreachable: the last logical line is the last row offsetToLineCol can name
		}
	}
	e.input.SetCursorColumn(col)
	e.input.SetHeight(e.input.Height())
}

// caretToOffset drives the caret to a BYTE offset into the current value — caretByteOffset's
// inverse, and what re-seats the caret after a completion splices over a token in the MIDDLE of a
// draft (acceptAutocomplete), where the widget's own MoveToEnd would jump to the wrong place.
// offsetToLineCol names the logical row and column; seatCaret walks to them. An offset past the end
// lands at the end.
func (e *lineEditor) caretToOffset(byteOff int) {
	value := e.input.Value()
	row, col := offsetToLineCol(value, runeOffsetOf(value, byteOff))
	e.seatCaret(row, col)
}

// caretToRune drives the caret to a RUNE offset into the current value — caretToOffset for a caller
// that counts in runes rather than in bytes, which is what a SELECTION counts in (promptSel) and what
// a click on a painted cell resolves to (cellToRuneOffsetIn, mouse.go). The conversion lives here so
// no caller has to pair the two functions itself and get the order right.
func (e *lineEditor) caretToRune(off int) {
	e.caretToOffset(byteOffsetOf(e.input.Value(), off))
}

// deleteSelection cuts a drag-selection's span out of the value and leaves the caret where that span
// began. It is what Backspace and Del mean while the field holds a highlight: the human can SEE what
// is selected, so the destructive keys must take exactly that rather than the one rune beside the
// caret (ISSUES.md — the highlight used to vanish and the selected text survive).
//
// The span arrives as an argument rather than being read off the caller's selection state because
// handleKey's chokepoint has already dropped the live selection by the time the two keys are routed
// (model.go): what it stashed there is the authority, and passing it in keeps that the ONLY copy.
// The span is normalised first — a right-to-left drag stores head before anchor, the same posture
// selectionText copies under (mouse.go) — and sliced in RUNES, so a multi-byte selection loses whole
// characters instead of splitting one.
//
// The rebuild-and-reseat shape is removeCompletionToken's (autocomplete.go): SetValue over the two
// surviving flanks, then drive the caret back to the cut by offset, since the widget leaves its
// cursor wherever the new value put it. caretToOffset counts BYTES while a selection counts RUNES,
// so byteOffsetOf bridges the two — read against the NEW value, whose first lo runes are exactly
// the head that survived the cut.
func (e *lineEditor) deleteSelection(sel promptSel) {
	lo, hi := sel.anchorOff, sel.headOff
	if lo > hi {
		lo, hi = hi, lo
	}
	r := []rune(e.input.Value())
	lo = clampInt(lo, 0, len(r))
	hi = clampInt(hi, lo, len(r))
	e.input.SetValue(string(r[:lo]) + string(r[hi:]))
	e.caretToOffset(byteOffsetOf(e.input.Value(), lo))
}

// reseatInput re-clamps the textarea's internal scroll after a SetHeight changed the box's height.
// bubbles repositions the view only when the caret falls outside it, so a box that auto-grows keeps
// a stale downward offset — the first content line scrolls out of sight with a phantom blank row
// below (ISSUES #2). Re-seating the caret where it already stands is the whole fix: seatCaret
// unscrolls to the top and walks back down, which re-clamps the offset to the current height and
// leaves the caret exactly where it was. layout() calls this only on a height change, which never
// happens during vertical caret navigation, so the textarea's remembered goal column is untouched.
func (e *lineEditor) reseatInput() {
	e.seatCaret(e.input.Line(), e.input.Column())
}
