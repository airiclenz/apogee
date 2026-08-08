package tui

import (
	"errors"
	"image/color"
	"os/exec"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// /settings — the full-height configuration pane (ADR 0035)
// ----------------------------------------------------------------------------
//
// The picker's and the browser's third sibling, and the frame's first FULL-HEIGHT pane: a modal
// list of every config key with the value THIS run resolved for it, painted through the shared popup
// module (renderPopup) and claiming every keypress while it is open (handleKey routes to
// settingsKey first). What it lists comes from the binary's declarative key registry across ONE
// seam ([Options.SettingsRows]) — the renderer holds no schema, reads no file and knows no
// precedence, exactly as it holds no session format behind [Options.Sessions] (ADR 0011's thin
// renderer).
//
// The rows are DERIVED on every call, the picker's own posture: the provider is asked at each
// render and at each keypress, and the selection is clamped against what came back rather than
// against a list captured at open, so a list that changed under an open pane can never be indexed
// past its end. What the provider answers is the resolution THIS run made, which is why a row this
// pane has EDITED shows what the pane wrote rather than waiting for the provider to catch up — with a
// ` *` saying so: the pane holds no copy of the config that could disagree with the file, only a
// journal of what it changed itself.
//
// It is the one pane granted the WHOLE transcript row budget (frameRowPlan, layout.md): the registry's
// keys and their section headers are not a choice to scan but a screen to read, so the conversation gives
// way entirely rather than the list scrolling inside eight rows. Nothing else about the frame moves —
// the four-row pane floor, the twelve-row window below which no pane is drawn, the status line, the
// input box and the footer are all where they were — and when the frame cannot seat the pane at all
// the fact goes on the status line (settingsGiveWayNote), the licence layout.md gives every surface
// that disappears.
//
// Everything the pane needs in order to say what a row IS — the effective value, the override
// marker, the mask, the read-only row's pointer — is already on the row (see [SettingRow]), so
// nothing here formats a config value or decides what may be written.
//
// What it does own is the SHAPE of the screen those rows are read on
// (docs/layout/settings-screen-layout.md): a fixed two-line description header naming what the key
// under the cursor is for (settingsBody), the sections set off by a blank line and labelled in white
// above the faint rows they open (settingsDisplayRows), and the row being typed into lit in the edit
// tone. All three are stated to the popup module rather than drawn here — the pane composes rows and
// a body, the module paints them (popupSpec.rowKinds, popupSpec.bodyLead) — so /settings looks like
// every other overlay in the frame and differs only where it says something the others do not.
//
// And it WRITES, one key per deliberate edit (ADR 0035): ⏎ on a bool row toggles it, ⏎ on an enum row
// opens the value sub-list, and what is committed goes out through [Options.WriteSetting] — the
// binary's comment-preserving splice writer, never this package's idea of YAML. The renderer's whole
// half of an edit is the ORDER (which key, which value) and what the row says afterwards. Nothing is
// re-read to find out: the pane records what it persisted ([Model.settingEdits]) and marks the row,
// because every other row is still showing the value THIS run resolved and a mid-session file read
// would leave one row disagreeing with its neighbours about which run it is describing.
//
// And what is persisted is APPLIED, on the same ⏎ (ADR 0037 decision 1): the pane routes the key to
// whatever puts it into effect — a field of its own for the keys whose effect is this screen, and
// [Options.ApplySetting] for every key the engine or the composition root owns — so the session runs
// what the file says the moment the file says it. A key that can only land at a boundary the session
// will cross anyway says so on the row ("· applies at next clear"); a key that could only land at the
// next start does not exist. What a row keeps afterwards is a ` *` (settingsEditMarker), which says
// this session changed the key here rather than that anything is still pending.
//
// A string or an int is edited in a BUFFER on its own row (the /sessions rename idiom): ⏎ opens it,
// the row's value cell becomes what is being typed with a caret after it, ⏎ commits and esc
// abandons. What a commit is checked against is the BINARY's business, exactly as the file format is
// — [Options.WriteSetting] refuses a value the key cannot hold (a port outside its range, an
// endpoint with no host) and the refusal lands on the row with the buffer still open, so the human
// corrects what they typed rather than typing it again.
//
// And backspace UNSETS: it arms a reset on a row that has something to reset, the hint line asks for
// a confirming ⏎, and what that ⏎ sends is [Options.ResetSetting] — the key's line REMOVED from the
// file rather than today's spelling of its default written into it (ADR 0035). The row then reports
// the default it went back to, on exactly the terms a write reports its value — and the default is
// APPLIED exactly as a written value is, so a reset cannot mean less to the session than a write.

// settingsKind is what the open pane is DOING: reading its key list, asking which value one enum key
// should take, holding the buffer a string or an int is being typed into, waiting for a reset to be
// confirmed, or holding the multi-line field a text key's prose is written in. It is the picker's own
// two-step idiom (pickerKind, /schedule's cycle-then-mode pair): one pane, one selection per step, and
// the step is a field rather than a second overlay — so there is no state in which two settings
// surfaces are open and no second give-way rule to write.
//
// One field for all five is also what makes them mutually exclusive by construction: a pane cannot be
// buffering a value and awaiting a reset confirmation at once, so no keypress has two meanings and no
// state pair has to be reasoned about.
type settingsKind int

const (
	settingsKeyList     settingsKind = iota // the key list — the pane's own screen
	settingsEnumList                        // the selected enum key's closed vocabulary, one value per row
	settingsValueBuffer                     // the selected string/int key's edit buffer, on its own row
	settingsResetArmed                      // backspace armed the selected row's reset; ⏎ confirms it
	settingsTextEditor                      // the selected text key's prose, in a multi-line field filling the pane
)

// settingsPane is the overlay's inline state on the Model. Its zero value is "closed", so it lives
// inline in the value-copied Model like [picker] and [sessionBrowser] (ADR 0011): plain values only,
// never a self-referential type held by value. selected indexes the SETTING rows the provider returns
// — not the display rows the pane paints, which interleave unselectable section headers — and it is
// clamped rather than trusted, because the list underneath it is re-derived on every key and every
// frame.
//
// Everything on it dies with the overlay, which is exactly what opening and closing it assign. failure
// is the pane's memory of the last write it was REFUSED, and it is display-only: it says what a refusal
// said, never what the config now holds (the provider answers that, from the resolution this run made).
// The journal of what this surface has CHANGED is not here — it outlives the overlay and so lives a
// level up, on the Model ([Model.settingEdits]). sub is the value sub-list's highlight, meaningful only
// while kind is [settingsEnumList]; editor is the field a string or an int is typed into, meaningful
// only while kind is [settingsValueBuffer].
//
// The editor is a [lineEditor] held BY VALUE, as the Model holds the prompt's own (ADR 0011): the
// widget is copy-safe and carries no self-referential no-copy type, which is what lets the pane stay
// a plain value on a Model that is copied every Update. Its zero value is an inert widget — a
// textarea nothing has focused, which answers "" and takes no keys — so a closed pane is still
// exactly the zero value, and no state outside settingsValueBuffer can type into anything.
//
// sel is a drag-selection inside that field, and it is the pane's own rather than the Model's
// ([Model.sel]): the prompt box is still drawn under this pane, so a span stored in the prompt's slot
// would be highlighted down there, on a box the pane's own modality keeps the human out of. It is a
// [promptSel] because it IS one — the same two ends of the same kind of field — but only the two RUNE
// offsets are carried: the caret is a GLYPH inside the painted cell here (settingsCaret), so it moves
// the text under a drag, and a visual cell recorded at the press would name a different rune a moment
// later. The highlight derives its columns from the offsets at paint time instead
// ([Model.highlightSettingsEdit]).
type settingsPane struct {
	open     bool
	kind     settingsKind
	selected int
	sub      int
	editor   lineEditor
	sel      promptSel
	failure  settingFailure
	answer   settingAnswer
}

// settingEdit is one key this pane PERSISTED this session and the value the file now yields for it —
// the fact behind a row's ` *` marker (ADR 0037 decision 8). It is not a cache of the config: the file is
// authoritative and the pane never reads it back, so this is only ever used to say "you changed this,
// here to what".
//
// reset marks the one edit that did not WRITE a value: a reset removed the key's line, so the value
// recorded is the DEFAULT the key went back to (empty when it defaults to unset) rather than anything
// the human typed. The row's marker needs the difference — a reset of a masked key has no secret to
// keep quiet about, and an empty value it returned to is spelled "unset" rather than as a blank.
//
// note is what the APPLY had to say about the value — the boundary sentence for a key that lands at
// the next session boundary rather than at once ("applies at next clear"), empty for a key already
// in force, which is almost all of them. It is carried on the edit rather than in a slot of its own
// because it describes THIS key's landing and stays true for as long as the edit does.
type settingEdit struct {
	path  string
	value string
	note  string
	reset bool
}

// settingFailure is the last write this pane was REFUSED, and by what — a read-only config home, a
// key the registry will not let a surface write, a file shape the splice would not risk. One slot,
// not one per row: it describes the last attempt rather than a row's condition, and the next attempt
// replaces it whatever that attempt does. An empty path means the last write landed (or none was made).
type settingFailure struct {
	path string
	msg  string
}

// settingAnswer is the pane's other outcome slot: an act that LANDED and left the row exactly as it
// was. It is neither a change nor a refusal, so it can be neither of the two things a row already
// says — a ` *` claiming this session changed the key, or a ✗ claiming the act was refused.
//
// Two acts have that shape. One is the `server` row's: choosing the server the session is already on
// is answered rather than acted on ([Model.switchToServer]), and the answer goes to the TRANSCRIPT,
// which this full-height pane is covering, so a ⏎ that is honestly a no-op looks from inside the pane
// like a keypress that did nothing at all. The other is a detached editor launch, which moves no row
// either and whose window may not even be on this screen ([Model.foldDetachedEdit]). The row says it
// in both cases, and that is all this slot is for.
//
// Its lifetime is settingFailure's: one slot describing the last act rather than a row's condition,
// replaced by the next landed edit (recordSettingEdit) or the next refusal (settingsFailed), and gone
// with the overlay.
type settingAnswer struct {
	path string
	msg  string
}

// settingsAlreadyOnNote opens the one answer this pane has, on the row that asked for it: the name of
// the server the session is already running against completes the sentence.
const settingsAlreadyOnNote = "already on "

// settingsTitle names the pane, and the hints are the one-line key legends at its foot — one per
// step, because the keys mean different things in each: in the key list ⏎ opens the selected row's
// edit idiom and esc leaves the pane, while in a value sub-list ⏎ COMMITS the highlighted value and
// esc backs out of the question without writing anything.
// The buffer's legend names the two keys that end it and nothing else — what a caret on a row already
// says is that the row is being typed into — and the reset's is the one line the pane ASKS anything:
// backspace armed something destructive, so the hint is where the confirmation is spelled out, which
// is the /sessions delete-confirm posture with ⏎ in place of y.
//
// The key list's own legend names the reset as well as the two keys that open and close the pane
// (docs/layout/settings-screen-layout.md's hint is a minimum, not an exhaustion): backspace is the
// only act of this pane that is not discoverable from the row it works on, and a key that removes a
// line from a file the human maintains by hand is exactly the one worth spelling out.
// On a row that takes no reset at all it names the two keys and stops (settingsNoResetHint): a legend
// is what the human reads backspace's meaning off, so advertising it over the one row where it does
// nothing is worse than saying nothing — they would press it and read the silence as a bug. The
// distinction is the KIND's and not the moment's: a row that merely has nothing to reset right now
// (settingsResettable) is one edit away from having something, and a legend flickering per value
// would be unreadable. A READ-ONLY row keeps the full legend for the reason the legend names the
// reset in the first place — its own cell already says where it is edited
// ([SettingRow.EditPointer]), so backspace doing nothing there is not the only thing saying so.
// The text editor's legend names the two keys that END it and nothing else, exactly as the buffer's
// does — and it has to be read, because they are not the keys every other step of this pane ends on:
// ⏎ belongs to the VALUE there (it inserts a newline in prose that has lines), so the commit moves to
// ctrl+s and the abandon stays on esc (ADR 0037 decision 10).
const (
	settingsTitle       = "Settings"
	settingsHint        = "↑/↓ select · ⏎ edit · ⌫ reset · esc close"
	settingsNoResetHint = "↑/↓ select · ⏎ edit · esc close"
	settingsEnumHint    = "↑/↓ select · ⏎ set · esc back"
	settingsBufferHint  = "⏎ save · esc cancel"
	settingsResetHint   = "⏎ confirm reset · esc cancel"
	settingsTextHint    = "ctrl+s save · esc discard"
)

// The pane's description header: the label its first line opens with, and the number of lines the
// description itself is allowed to take under it. Two lines because a registry description is a
// sentence and a sentence rarely fits one at eighty columns — and no more than two because the
// region is FIXED (ADR 0037 decision 9): every row's description is composed to exactly this height,
// so moving the selection down the list moves nothing else on the screen. What a longer description
// loses is its tail, to an ellipsis in the painter's own measure (truncateToWidth), rather than the
// list losing a row to it.
const (
	settingsDescLabel = "Description:"
	settingsDescLines = 2
)

// settingsCaret is the glyph drawn AT the caret in an open edit buffer — the /sessions rename idiom's
// own, now standing wherever the caret does rather than always after the last rune: the value is
// edited in a real field ([lineEditor]), so there is a position to draw, and drawing it is how the row
// says where the next keystroke lands.
//
// It is a glyph in the cell rather than the terminal's own cursor because the pane paints through the
// popup module, whose cells are plain escape-free text it styles whole (doc.go) — there is no seat on
// a popup row for the real cursor the chat box gets (lineEditor.textWithCaret).
const settingsCaret = "▏"

// settingsValueColumn is which column of a row the VALUE is laid out in — the second, by the fixed
// schema settingRowCells composes ("key", "value", "(env)", "· note"). It is stated once because the
// MOUSE needs it: a click seating the caret in the edit field has to know which cell of the painted
// row that field is, and counting the schema out a second time is how the two come to disagree.
const settingsValueColumn = 1

// settingsUnsetValue is how the value cell spells a value that is not there — the state a reset
// returns a key with no built-in default to. "" would leave the marker floating after a blank cell,
// which is the one thing the row must not do after a deliberate act.
const settingsUnsetValue = "unset"

// settingsEditMarker is the suffix on the value cell of every key this session changed THROUGH THIS
// SURFACE — an in-pane edit or a reset — and the whole of what a row says about having been changed
// (ADR 0037 decision 8). It replaces the deferral markers this pane used to paint: an edit APPLIES on
// the ⏎ that persists it, so there is no pending value to point at and nothing to wait for, and what
// is left worth saying is which rows this session touched. It is cleared only by a relaunch, because
// the journal behind it is the SESSION's memory rather than the overlay's ([Model.settingEdits]): it
// survives every close and reopen of the pane, as the edit it stands for survives them.
const settingsEditMarker = " *"

// The value cells of a bool row, spelled as the config file spells them — the two strings ⏎ toggles
// between and hands [Options.WriteSetting], which is the whole of what "the value as the file would
// spell it" means for a bool (see [SettingRow.Value]).
const (
	settingTrue  = "true"
	settingFalse = "false"
)

// noSettingsWriterNote is what a row says when ⏎ has nothing to write with: a build or a Driver that
// composed [Options] without the write seam (ADR 0031). It sits where a write error would, because
// for the human it IS one — the edit did not happen and the reason is not theirs to fix — and it says
// it on the row rather than in the transcript, which a full-height pane is covering.
const noSettingsWriterNote = "cannot write config in this build"

// noSettingsNote is the one honest line /settings owes when there is nothing to show: no provider
// wired (a hand-built Options, or a Driver that composes the engine without the binary's registry —
// ADR 0031), or a provider that came back with no rows. Both are worded the same on purpose, the
// noServersNote posture: for the human they are ONE situation — this build cannot tell them what it
// resolved — and two sentences would only invite the two to drift. A note and no overlay, like every
// other degrade in this package: an empty pane would say less than the sentence explaining it.
const noSettingsNote = "settings are unavailable — no configuration is wired"

// noSettingsRow is what an open pane shows if the provider stops answering under it — the browser's
// empty-view row, one unselectable cell. Reaching it means the rows went away while the pane was up,
// which the degrade above keeps from ever being the opening state.
const noSettingsRow = "no configuration to show"

// settingsSourceMarker is the "(env)" | "(flag)" cell a row earns when a higher-precedence source
// beat the config file for that key THIS run ([SettingRow.Source]). It is a cell of its own rather
// than a suffix on the value, the currentRowCell posture: the popup module styles rows whole and
// aligns them by column, so the marks of every overridden row land in one column and the column
// collapses away entirely on a config nothing is overriding.
func settingsSourceMarker(source SettingSource) string {
	if source == SettingFromFile {
		return ""
	}
	return "(" + string(source) + ")"
}

// runSettingsCommand drives the /settings verb: it opens the pane, or — with no rows to show —
// notes why not and opens nothing. Idle-only by the commandSpecs table and synchronous like
// /sessions: no upstream call, no worker, and no config read of its own (the provider is the
// binary's).
//
// The provider is called HERE as well as at render, and deliberately: "is there anything to show"
// is the one question the answer to which decides whether a modal that swallows every key goes up
// at all, and asking it at open is what keeps the human from having to press esc to escape an empty
// pane.
func (m Model) runSettingsCommand() (tea.Model, tea.Cmd) {
	if len(m.settingRows()) == 0 {
		m.transcript.addNote(noSettingsNote)
		m.layout()
		return m, nil
	}
	m.settings = settingsPane{open: true}
	m.layout()
	return m, nil
}

// settingRows is the pane's rows as they stand RIGHT NOW: what [Options.SettingsRows] answers, or
// nothing at all when no provider is wired. Every reader goes through here — the open degrade, the
// key routing's clamp, the renderer — so the count the selection is clamped against and the list the
// pane paints are the same derivation asked twice rather than two guesses at it.
func (m Model) settingRows() []SettingRow {
	if m.opts.SettingsRows == nil {
		return nil
	}
	return m.opts.SettingsRows()
}

// settingsKey routes a keypress while the pane is open (idle only, the verb's own policy): ↑/↓ move
// the highlight, wrapping at both ends (the pickerKey idiom), ⏎ opens the selected row's edit idiom,
// backspace arms its reset and esc closes. Every other key is SWALLOWED, because the pane is modal: a
// keystroke that fell through to the input box would edit a draft the human cannot see behind a
// full-height pane.
//
// The row count is re-derived and the selection re-clamped on every key rather than once at open,
// the picker's posture: the provider answers from the binary's live resolution, so the list can
// legitimately change under an open pane (a persisted edit is exactly that), and a selection left
// pointing past the end of a shorter list would be an index panic one keypress later.
//
// A second step — a value sub-list, an edit buffer, an armed reset — claims the keys FIRST and its
// target row is re-derived with them, so the step is always about a row that is still there: a step
// whose key went away under it falls back to the key list rather than committing a value to whatever
// now sits at that index.
func (m Model) settingsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Any keypress drops a drag-selection in the edit field, for handleKey's own reason one surface
	// down (model.go): typing past a span, committing it or walking away from it all move on from what
	// was selected, and clearing at the pane's one chokepoint keeps every branch below from having to
	// remember to.
	m.settings.sel = promptSel{}
	rows := m.settingRows()
	n := len(rows)
	m.settings.clampSelection(n)
	if m.settings.kind == settingsEnumList {
		if row, ok := m.settingsEnumTarget(rows); ok {
			return m.settingsEnumKey(msg, row)
		}
		// The sub-list's key went away under it. Drop back to the key list and SWALLOW this keypress
		// rather than let it through: a ⏎ aimed at a value must not land on whatever key now sits at
		// that index, and the human's next press is aimed at a list they can see.
		m.settings.kind, m.settings.sub = settingsKeyList, 0
		m.layout()
		return m, nil
	}
	if m.settings.kind == settingsValueBuffer {
		if row, ok := m.settingsBufferTarget(rows); ok {
			return m.settingsBufferKey(msg, row)
		}
		return m.settingsAbandonStep() // the buffered key went away — same fallback, same reason
	}
	if m.settings.kind == settingsTextEditor {
		if row, ok := m.settingsTextTarget(rows); ok {
			return m.settingsTextKey(msg, row)
		}
		return m.settingsAbandonStep() // the key being written went away — same fallback, same reason
	}
	if m.settings.kind == settingsResetArmed {
		if row, ok := m.settingsResetTarget(rows); ok {
			return m.settingsResetKey(msg, row)
		}
		return m.settingsAbandonStep()
	}
	switch msg.String() {
	case "esc":
		// The overlay goes whole — highlight, step, buffer, last refusal — and the session's edit
		// journal does NOT: it is on the Model, and the ` *` markers it carries describe a session that
		// is still running the values it recorded (ADR 0037 decision 8).
		m.settings = settingsPane{}
		m.layout()
		return m, nil
	case "up", "ctrl+p":
		if n > 0 {
			m.settings.selected = (m.settings.selected - 1 + n) % n
		}
		return m, nil
	case "down", "ctrl+n":
		if n > 0 {
			m.settings.selected = (m.settings.selected + 1) % n
		}
		return m, nil
	case "enter":
		return m.settingsEnter(rows)
	case "backspace":
		return m.settingsArmReset(rows)
	}
	return m, nil // any other key is swallowed by the modal
}

// settingsOwnsInput reports whether the pane is the surface the keyboard is on: open, at the one
// state its verb runs in. It is the gate [Model.handleKey] routes keypresses by, stated once because
// the two message kinds that are TEXT rather than keystrokes have to be routed by exactly the same
// rule (settingsPaste, settingsEditorMsg) — a paste that followed a different rule from the keys
// would land in the draft behind a pane the human cannot see past.
func (m Model) settingsOwnsInput() bool {
	return m.state == stateIdle && m.settings.open
}

// settingsPaste routes a bracketed paste while the pane owns the keyboard: into the field when one is
// open, and NOWHERE when none is. The second half is the pane's modality applied to the one edit that
// does not arrive as a keypress — the pane is full-height, so a paste falling through to the chat box
// would fill a draft the human cannot see (handleKey's own reason for swallowing every key it does not
// act on).
func (m Model) settingsPaste(msg tea.PasteMsg) (Model, tea.Cmd, bool) {
	if next, cmd, claimed := m.settingsEditorMsg(msg); claimed {
		return next, cmd, true
	}
	return m, nil, m.settingsOwnsInput()
}

// settingsEditorMsg hands a Msg that is TEXT to whichever field the pane has open — the terminal's
// bracketed paste, and the clipboard reply the widget's own ctrl+v asks for, which is a Msg of the
// widget package's own unexported type and can therefore be recognised by nothing but the route it
// took ([lineEditor.editMsg]). claimed is false wherever there is no field to type into, which leaves
// the chat box's own messages to the chat box.
//
// The value buffer flattens what arrives and the multi-line field does not — the field's own
// invariant, imposed where the text enters it (lineEditor.editMsg) — and only the multi-line field
// lays the frame out again, for settingsTextKey's reason: it IS the pane's row list, so a pasted line
// changes how many rows the pane measures, while a value buffer is one cell of one row.
func (m Model) settingsEditorMsg(msg tea.Msg) (Model, tea.Cmd, bool) {
	if !m.settingsOwnsInput() {
		return m, nil, false
	}
	switch m.settings.kind {
	case settingsValueBuffer:
		// The value is about to change under the highlight, so the span goes first — settingsKey's
		// chokepoint rule for the edits that arrive as keystrokes, and the same one here.
		m.settings.sel = promptSel{}
		return m, m.settings.editor.editMsg(msg), true
	case settingsTextEditor:
		m.settings.sel = promptSel{}
		cmd := m.settings.editor.editMsg(msg)
		m.layout()
		return m, cmd, true
	}
	return m, nil, false
}

// settingsAbandonStep drops a second step whose row went away and swallows the keypress that found
// it gone — the enum sub-list's fallback, shared by the buffer and the armed reset. Nothing is
// written and nothing is kept: a buffer whose key is no longer there has nothing to save, and a
// confirmation for a row that left cannot be confirmed.
func (m Model) settingsAbandonStep() (tea.Model, tea.Cmd) {
	m.settings.kind, m.settings.sub, m.settings.editor = settingsKeyList, 0, lineEditor{}
	m.layout()
	return m, nil
}

// settingsEnter opens the selected row's edit idiom — the one place the pane decides what ⏎ MEANS,
// which is the row's kind and nothing else:
//
//   - a bool is toggled and persisted on the spot, because a two-value key has no question to ask;
//   - an enum asks which value, in a sub-list of its own (the /schedule two-step) — and so does the
//     `server` row, whose vocabulary is this config's own `servers:` block ([SettingServer]);
//   - a string or an int opens a buffer on the row, seeded with what the key holds;
//   - a text key's prose opens a multi-line field over the whole list, seeded with the same
//     ([SettingText]) — the one step whose ⏎ is the value's rather than the pane's; and
//   - a row the registry does not let this surface write does nothing at all — its own cell already
//     says where it IS edited ([SettingRow.EditPointer]), so a refusal note here would only repeat it.
//
// rows is the list the keypress was clamped against, passed in rather than re-derived, so the row
// acted on is the row the frame the human pressed ⏎ at was showing.
func (m Model) settingsEnter(rows []SettingRow) (tea.Model, tea.Cmd) {
	sel := m.settingsSelection(len(rows))
	if sel < 0 {
		return m, nil
	}
	row := rows[sel]
	if !row.Editable {
		// A block this pane cannot hold on a row is edited where it CAN be edited: the human's own
		// editor, opened on the key's line (ADR 0037 decision 5). Every other read-only row still does
		// nothing at all — the confinement pair's cell already says where their interlock lives.
		if row.ExternalEdit {
			return m.settingsExternalEdit(row)
		}
		return m, nil
	}
	switch row.Kind {
	case SettingBool:
		return m.settingsWrite(row, toggledSetting(m.settingsPersistedValue(row)))
	case SettingEnum, SettingServer:
		values := m.settingsVocabulary(row)
		if len(values) == 0 {
			// Nothing to offer: an enum with no vocabulary (the registry pins this), or a `servers:`
			// block that names nothing to switch to (the noServersNote case, one pane over).
			return m, nil
		}
		// The sub-list opens ON the value the key holds, not at the top of the list: the human who
		// presses ⏎ twice has then confirmed what was already set — which saveConfigSetting writes
		// nothing for — where a highlight reset to the first row would have silently changed the key.
		m.settings.kind = settingsEnumList
		m.settings.sub = max(0, indexOfSetting(values, m.settingsCurrentValue(row)))
		m.layout()
		return m, nil
	case SettingString, SettingInt:
		m.settings.kind = settingsValueBuffer
		m.settings.editor = newSettingsEditor(m.opts.CursorShape, m.th.surface, m.settingsBufferSeed(row))
		m.layout()
		return m, nil
	case SettingText:
		m.settings.kind = settingsTextEditor
		m.settings.editor = newSettingsTextEditor(m.opts.CursorShape, m.th.surface, m.settingsTextValue(row))
		m.layout()
		return m, nil
	case SettingStructured:
		return m, nil // never Editable; the registry terminates descent here
	}
	return m, nil // an unknown kind is read-only, the safe end ([SettingKind])
}

// settingsEnumKey routes a keypress in the value sub-list: ↑/↓ walk the vocabulary with the same wrap
// the key list uses, ⏎ persists the highlighted value, and esc backs out having written nothing —
// which is the whole reason the enum edit is two steps rather than a cycle in place.
//
// The sub-list closes on ⏎ whether the write lands or is refused: the question was answered, and a
// refusal belongs on the row that asked it (settingsNote), where it is still on the screen after the
// pane returns to its list.
//
// What ⏎ then DOES is the row's, not this function's: every enum is persisted and applied
// (settingsWrite), while the `server` row is switched (settingsSwitchServer) — the one row whose
// value is not written by this pane at all.
func (m Model) settingsEnumKey(msg tea.KeyPressMsg, row SettingRow) (tea.Model, tea.Cmd) {
	values := m.settingsVocabulary(row)
	n := len(values)
	switch msg.String() {
	case "esc":
		m.settings.kind, m.settings.sub = settingsKeyList, 0
		m.layout()
		return m, nil
	case "up", "ctrl+p":
		m.settings.sub = (m.settings.sub - 1 + n) % n
		return m, nil
	case "down", "ctrl+n":
		m.settings.sub = (m.settings.sub + 1) % n
		return m, nil
	case "enter":
		value := values[clampInt(m.settings.sub, 0, n-1)]
		m.settings.kind, m.settings.sub = settingsKeyList, 0
		if row.Kind == SettingServer {
			return m.settingsSwitchServer(row, value)
		}
		return m.settingsWrite(row, value)
	}
	return m, nil // swallowed, like every key the pane does not act on
}

// settingsSwitchServer answers the `server` row's popup, and what it does is the whole of `/server`
// (ADR 0037 decision 4): the session MOVES to the chosen entry, and the move records that entry as
// the one the next session starts on — which is this key's entire persistence, so no WriteSetting
// call stands behind this row (ADR 0036 decision 2).
//
// Two of the outcomes are the picker's own and are delegated rather than restated, so a switch driven
// from this pane and one driven from `/server` cannot answer differently: a PRE-BOUND session
// constructs its engine instead of moving one ([Model.bindToServer]), and choosing the server the
// session is already on is answered rather than acted on. Neither is an edit of the key, so neither
// is journaled — the first is a first binding and the second changed nothing. The second does get its
// answer repeated on the ROW ([settingAnswer]), because the delegate says it in a transcript this
// full-height pane is covering.
//
// What this path owns is where a refusal goes. The seam is validate-then-commit, so an error means
// the session did not move, and the sentence belongs on the ROW that asked (settingsNote) rather
// than in a transcript this pane is covering. A move that landed is journaled, so the row shows the
// server now on the wire with the ` *` that says this session chose it; everything the human sees of
// the move itself — the restated start-up box, the switching note, the recording — is
// [Model.foldServerSwitch]'s, and this pane adds nothing to it.
func (m Model) settingsSwitchServer(row SettingRow, name string) (tea.Model, tea.Cmd) {
	choice, ok := serverNamed(m.servers(), name)
	if !ok {
		// The popup is fed from this very list, so this is a list that moved under an open question.
		return m.settingsFailed(row, "unknown server: "+stripEscapes(name))
	}
	if m.prebound() {
		return m.switchToServer(choice)
	}
	if choice.Endpoint == m.opts.Endpoint {
		// The delegate's answer is a transcript note, and this pane is drawn over the transcript — so
		// from in here the ⏎ that confirmed the current server would look like a keypress that did
		// nothing. The same sentence lands on the row (settingAnswer), where the human who pressed it is
		// looking. It is not a failure and not an edit: nothing was refused and nothing changed, which is
		// why it clears the failure slot rather than filling it.
		m.settings.answer = settingAnswer{path: row.Path, msg: settingsAlreadyOnNote + stripEscapes(choice.Name)}
		m.settings.failure = settingFailure{}
		return m.switchToServer(choice)
	}
	if m.opts.SwitchServer == nil {
		return m.settingsFailed(row, noServerSwitchNote)
	}
	from := hostDisplay(m.opts) // the label the footer used for the old server, captured before it moves
	result, err := m.opts.SwitchServer(choice.Name)
	if err != nil {
		return m.settingsFailed(row, stripEscapes(err.Error()))
	}
	m = m.recordSettingEdit(settingEdit{path: row.Path, value: choice.Name})
	return m.foldServerSwitch(from, result, recordServerChoice(m.opts.RecordServerChoice, choice.Name))
}

// settingsFailed puts msg on row as this pane's one failure slot and repaints — the outcome shape
// every refused act in the pane ends in (settingsNote paints it, the next landed edit clears it).
func (m Model) settingsFailed(row SettingRow, msg string) (tea.Model, tea.Cmd) {
	m.settings.failure = settingFailure{path: row.Path, msg: msg}
	m.settings.answer = settingAnswer{} // the last act's outcome, replaced by this one's
	m.layout()
	return m, nil
}

// noServerSwitchNote is what the `server` row says when the binary wired no switch seam — the
// nil-seam degrade noSettingsWriterNote gives every other row, worded for the act this one performs.
const noServerSwitchNote = "cannot switch server in this build"

// settingsEditedMsg is the return of a FOREGROUND external editor: which row launched it, and
// whether the process itself ran. It is the pane's own message rather than a shared one because
// nothing else in the frame suspends the program — the path is carried so the reload's outcome lands
// on the row the human pressed ⏎ on, which is the only row they have any reason to be looking at.
//
// A detached launch has no such return: nobody waits for it, so the message it produces is about the
// START and nothing else ([settingsDetachedMsg]).
type settingsEditedMsg struct {
	path string
	err  error
}

// settingsDetachedMsg is what a DETACHED launch has to say: it started, or it did not. There is no
// exit to report and no re-read to trigger — the pane never left, the program outlives the keypress,
// and what the human writes in it arrives by the config watcher instead (ADR 0041 decision 3).
//
// It is a message rather than a value the keypress folds in place because starting a process is
// work, and work belongs on a Cmd goroutine rather than in Update — the same reason the foreground
// path hands its process to Bubble Tea.
type settingsDetachedMsg struct {
	path string
	err  error
}

// The two sentences the external edit refuses with. The first is binding C of ADR 0037: suspending
// the whole program into an editor while a Step is streaming would take the transcript off the
// screen mid-answer and leave the applies to queue behind a run — so the offer stands only between
// runs, and the row says which. The second is the nil-seam degrade noSettingsWriterNote gives every
// other row, worded for the act this one performs.
const (
	settingsEditBusyNote = "wait for the current run to finish"
	noExternalEditNote   = "cannot open an editor in this build"
)

// settingsDetachedEditNote is what the row says when the editor was started DETACHED. The pane never
// went away, so a keypress that opened a window somewhere else — behind the terminal, on another
// desktop, in an application that was already running — looks from in here like a keypress that did
// nothing at all. This sentence is the whole of what the row has to show for it, which is why the
// launch lands in the pane's answer slot rather than silently ([settingAnswer]).
const settingsDetachedEditNote = "opened in your editor"

// settingsExternalEdit answers ⏎ on a row holding a structure no field can express: it opens the
// human's own editor on that key's line (ADR 0037 decision 5). The command line is the binary's —
// which file, which line, which editor, and whether that editor takes this terminal — and this only
// runs it ([Options.ExternalEditSpec]).
//
// Two ways to run it, one keypress (ADR 0041 decision 6). A FOREGROUND editor gets the program's own
// terminal through tea.ExecProcess and its exit is the trigger for the re-read, exactly as it has
// always been — a terminal editor drawn over a live alt-screen TUI is broken, so there is no other
// way to run one. Everything else is started DETACHED: the pane stays up, nothing waits, and what
// the human saves arrives through the config watcher rather than through an exit that means nothing
// (an opener stub returns before the editor is even on screen).
//
// It is offered only between runs (binding C). Mid-run the row says to wait rather than the pane
// queueing the edit: the alternative is tearing the alternate screen down over a streaming reply and
// holding a file's worth of applies until it finishes, for a key nobody is waiting on that hard. In-
// pane edits stay allowed mid-run — those apply through the seams that know how to refuse.
//
// The in-flight test names both halves of "a run", and the LATCH is the half that reaches here: a
// streaming Step leaves this pane's keys unrouted entirely (the overlay is idle-only, handleKey), so
// it is a launcher actuation — which runs on a Cmd goroutine with the session idle — that a human can
// actually press ⏎ during. The busy check stands beside it because the sentence is about runs, not
// about which of them today's routing happens to deliver.
//
// A refusal from the spec is the row's failure, and NOTHING is launched: an unreadable config or a
// file shape the parse will not risk is exactly the moment not to hand a human an editor and a
// promise to re-read it.
func (m Model) settingsExternalEdit(row SettingRow) (tea.Model, tea.Cmd) {
	if m.busy() || m.actuation.inFlight {
		return m.settingsFailed(row, settingsEditBusyNote)
	}
	if m.opts.ExternalEditSpec == nil {
		return m.settingsFailed(row, noExternalEditNote)
	}
	launch, err := m.opts.ExternalEditSpec(row.Path)
	if err != nil {
		return m.settingsFailed(row, stripEscapes(err.Error()))
	}
	argv := launch.Argv
	if len(argv) == 0 {
		return m.settingsFailed(row, noExternalEditNote)
	}
	// The last refusal goes with the keypress that acted past it: the human is leaving the screen —
	// or the screen is staying and the editor is opening elsewhere — and a ✗ from an earlier attempt
	// has nothing to say about the file they are about to edit.
	m.settings.failure = settingFailure{}
	m.layout()
	if launch.Detached {
		return m, startDetachedEditor(row.Path, argv)
	}
	return m, tea.ExecProcess(exec.Command(argv[0], argv[1:]...), func(err error) tea.Msg {
		return settingsEditedMsg{path: row.Path, err: err}
	})
}

// startDetachedEditor is the launch that keeps the terminal: the program is started with no stdin,
// stdout or stderr of ours — a nil stream in [exec.Cmd] is the null device, so the editor cannot
// write over the frame we are still drawing and cannot read the keys we are still routing — and
// nothing waits for it.
//
// The Wait in the background is not a wait FOR the editor, it is the reaping of it: a child nobody
// waits for stays a zombie in the process table for as long as apogee runs, and a human who opens
// their config a dozen times in a session should not leave a dozen of them. It carries no outcome —
// what the editor did to the file is the watcher's to notice, and its exit code is an answer to a
// question the pane stopped asking the moment it let go.
func startDetachedEditor(path string, argv []string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command(argv[0], argv[1:]...)
		if err := cmd.Start(); err != nil {
			return settingsDetachedMsg{path: path, err: err}
		}
		go func() { _ = cmd.Wait() }()
		return settingsDetachedMsg{path: path}
	}
}

// foldDetachedEdit is the whole of what a detached launch does to the pane: nothing, plus a sentence.
// A start that failed is this pane's one failure slot, on the row the human pressed ⏎ on — the same
// place every other refusal in here lands — and a start that worked is an act that landed and changed
// no row, which is what the answer slot is for ([settingAnswer]).
//
// No re-read follows either way. The editor is still open; the file is not the pane's to interpret
// until somebody saves it, and then it is the watcher that says so (ADR 0041 decision 3).
func (m Model) foldDetachedEdit(msg settingsDetachedMsg) (tea.Model, tea.Cmd) {
	rows := m.settingRows()
	launched, ok := settingRowOf(rows, msg.path)
	if !ok {
		launched = SettingRow{Path: msg.path}
	}
	if msg.err != nil {
		return m.settingsFailed(launched, stripEscapes(msg.err.Error()))
	}
	m.settings.answer = settingAnswer{path: msg.path, msg: settingsDetachedEditNote}
	m.layout()
	return m, nil
}

// foldSettingsEdit is what happens when the editor exits: the binary re-reads the file, and every
// key that came back different is journaled and applied — through the same two homes an in-pane
// commit uses (settingsApplied), so a key edited in the file and a key edited on its row land
// identically and neither has an apply path of its own.
//
// The editor's own failure ends the round trip without a re-read. A command that could not run
// changed nothing, and a non-zero exit is how an editor SAYS to discard (`:cq`) — re-reading over
// either would be answering a question the human declined to ask. Same for a reload that could not
// parse or validate what it found: the file is left exactly as they wrote it and the reason lands on
// the row they launched from, which is where they go back in from.
//
// Notes are per key, on the edit that earned them; a refusal is the pane's one failure slot, so a
// reload in which two keys both refused shows the last of them — the slot describes the last attempt
// rather than a row's condition ([settingFailure]), and this is one attempt.
func (m Model) foldSettingsEdit(msg settingsEditedMsg) (tea.Model, tea.Cmd) {
	rows := m.settingRows()
	launched, ok := settingRowOf(rows, msg.path)
	if !ok {
		launched = SettingRow{Path: msg.path}
	}
	if msg.err != nil {
		return m.settingsFailed(launched, stripEscapes(msg.err.Error()))
	}
	if m.opts.ReloadConfig == nil {
		return m.settingsFailed(launched, noExternalEditNote)
	}
	applied, err := m.opts.ReloadConfig()
	if err != nil {
		return m.settingsFailed(launched, stripEscapes(err.Error()))
	}
	m, cmds := m.applyReloaded(rows, applied)
	m.layout()
	return m, tea.Batch(cmds...)
}

// applyReloaded journals and applies every key a re-read found changed. It is the one apply loop the
// round trip's TWO triggers share (ADR 0041 decision 6: one apply path, two triggers) — an editor
// that exited, and a file the watcher saw change — so a key can never land one way when the human
// edited it in a terminal editor and another way when they saved it from a GUI one.
//
// What the applies ask for is BATCHED rather than kept one at a time: an edit that changed the colour
// scheme and the scroll bar in one session of the editor has to leave with the scheme's repaint still
// asked for.
func (m Model) applyReloaded(rows []SettingRow, applied []AppliedSetting) (Model, []tea.Cmd) {
	var cmds []tea.Cmd
	for _, a := range applied {
		row, ok := settingRowOf(rows, a.Path)
		if !ok {
			continue // a key the pane does not list has no row to journal it on
		}
		next, cmd := m.settingsApplied(row, settingEdit{path: a.Path, value: a.Value})
		m = next
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return m, cmds
}

// configChangedMsg is one report from the binary's config watcher: the file changed, or — alive
// false — the WATCH itself ended and nothing more will ever be reported. It carries no path and no
// keys, because the watcher knows neither: what changed is [Options.ReloadConfig]'s answer, and the
// message is only the news that it is worth asking (ADR 0041 decision 3).
type configChangedMsg struct{ alive bool }

// configWatchState is what the Model remembers between reports: how many re-reads in a row could not
// be made, and whether the human has been told about it. Two plain fields in a plain value, safe in
// the value-copied Model (ADR 0011).
type configWatchState struct {
	// fails counts CONSECUTIVE unreadable re-reads; a re-read that lands clears it.
	fails int
	// noted records that the note below has already been said for this run of failures, so the same
	// broken file cannot narrate itself once per save.
	noted bool
}

// configWatchStallReports is how many consecutive unreadable re-reads it takes before the transcript
// says so (ADR 0041 decision 7). Fewer would report the ordinary case — an editor whose save the
// watcher happened to catch mid-write — as a problem the human has to do something about.
const configWatchStallReports = 3

// configWatchStalledNote is what the transcript says when it does. It names the consequence rather
// than the event, because a file that does not parse is not itself news: what the human needs to know
// is that the session is NOT running what they just saved, and why.
const configWatchStalledNote = "the config file has not parsed for three saves, so the session is " +
	"still running the settings it had: "

// awaitConfigChange opens ONE wait on the binary's config watcher (ADR 0041 decision 3). It is the
// whole of this chain's arming: Init opens the first wait and each landed report opens the next, so
// there is exactly one wait outstanding at any moment (doc.go's tick-chain invariant — two would
// re-read the file twice for every save and apply everything twice).
//
// It takes the program context, as [Model.beatCmd] does, so a quit ends the wait where it stands
// rather than leaving it parked on a channel until the composition root's teardown reaches the
// watcher. nil seam ⇒ no Cmd and therefore no chain, the nil-seam degrade every provider here takes.
func (m Model) awaitConfigChange() tea.Cmd {
	await := m.opts.AwaitConfigChange
	if await == nil {
		return nil
	}
	ctx := m.parent
	return func() tea.Msg {
		return configChangedMsg{alive: await(ctx)}
	}
}

// foldConfigChanged is what a saved config file does to a running session: the same re-read, the same
// journal and the same applies an editor's exit produces (applyReloaded), for a save this program had
// nothing to do with (ADR 0041 decision 5).
//
// It runs whether or not the pane is open and whether or not a Step is streaming, and deliberately:
// the human saved a document, and the keys that cannot land right now refuse on their own rows
// through the very seams that know how to refuse — the same posture an in-pane commit takes mid-run.
// What it must NOT do is apply twice, which is what the baseline refresh on every pane write buys
// (ADR 0041 decision 8, in the binary): a key apogee itself just wrote comes back as no change at all.
//
// The next wait is opened before anything is applied, so a re-read that ends in a refusal still leaves
// the session watching — a broken config the human is about to fix is exactly the file the next report
// has to be about. A watch that has ENDED arms nothing: there is no report to wait for any more.
func (m Model) foldConfigChanged(msg configChangedMsg) (tea.Model, tea.Cmd) {
	if !msg.alive {
		return m, nil
	}
	next := m.awaitConfigChange()
	if m.opts.ReloadConfig == nil {
		return m, next
	}
	applied, err := m.opts.ReloadConfig()
	if err != nil {
		return m.foldConfigUnreadable(err), next
	}
	m.cfgWatch = configWatchState{}
	m, cmds := m.applyReloaded(m.settingRows(), applied)
	m.layout()
	return m, tea.Batch(append(cmds, next)...)
}

// foldConfigUnreadable is the last-good rule's half of the fold (ADR 0041 decision 7): a file that
// does not parse or does not validate applies NOTHING and moves nothing — the binary keeps the
// baseline it had, so the human's fix is still diffed against the config that was last good.
//
// The failure is silent until it has survived configWatchStallReports saves, and then it is said
// once. A watcher will inevitably read a file somebody is halfway through writing, so the first
// failures are normal and self-correcting; a note per report would be an error scrolling past every
// time somebody hits save while they are still typing. It goes to the TRANSCRIPT rather than to a row
// because there is no row: nobody pressed anything, and the pane is very likely not even open.
func (m Model) foldConfigUnreadable(err error) Model {
	m.cfgWatch.fails++
	if m.cfgWatch.fails < configWatchStallReports || m.cfgWatch.noted {
		return m
	}
	m.cfgWatch.noted = true
	m.transcript.addNote(configWatchStalledNote + err.Error())
	m.layout()
	return m
}

// settingRowOf finds the row for a registry path in the list as it stands — the lookup both halves
// of the round trip need, since what comes back from the binary is a path and what the journal and
// the apply are about is a row.
func settingRowOf(rows []SettingRow, path string) (SettingRow, bool) {
	for _, r := range rows {
		if r.Path == path {
			return r, true
		}
	}
	return SettingRow{}, false
}

// settingsBufferSeed is what a freshly opened edit buffer starts from: the value the pane believes the
// file holds, so a human correcting a port edits the port rather than retyping it — and NOTHING for a
// masked key, because the row carries a mask and not the secret ([SettingRow]). An api-key is
// therefore typed whole, which is the only honest offer a surface that never held the old one can
// make.
func (m Model) settingsBufferSeed(row SettingRow) string {
	if row.Masked {
		return ""
	}
	return m.settingsPersistedValue(row)
}

// newSettingsEditor is the field a string or an int row is typed into: a single-line [lineEditor]
// (lineeditor.go — the chat box's own field, minus the chat), seeded with what the key holds and with
// the caret at the end of it, which is where a human correcting a port starts.
//
// Single-line is the whole configuration it needs: ⏎ then belongs to this pane (commit) rather than to
// the widget, and a scalar config value has no second line to walk to. shape is the `cursor-shape`
// key's selection, passed for the same reason the prompt takes it — the field is built the same way
// wherever it is built.
func newSettingsEditor(shape tea.CursorShape, surface color.Color, seed string) lineEditor {
	e := newLineEditor(shape, surface)
	e.singleLine()
	e.setValue(seed)
	return e
}

// settingsBufferKey routes a keypress in the string/int edit buffer: ⏎ commits, esc abandons, and
// every other key goes to the FIELD (lineEditor.editKey) — which is the whole of the editing this
// pane does not write itself. What the human gets there is what the chat box gives: insertion at the
// caret, backspace and delete around it, ←/→ and the word jumps, home/end (spec requirement 7).
//
// The buffer is the one state in which backspace does not arm a reset: inside an edit it means what
// it means in every other text field on the screen. That is exactly why the two idioms can share the
// key — the pane's kind says which of them is being typed at, and the field claims backspace only
// while it is open.
func (m Model) settingsBufferKey(msg tea.KeyPressMsg, row SettingRow) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// An abandoned edit takes its refusal with it: the ✗ on this row is the reason THIS buffer
		// was not accepted, and leaving it up after the human walked away from the edit would report
		// a failure against a row nobody is editing any more.
		m.settings.kind, m.settings.editor = settingsKeyList, lineEditor{}
		m.settings.failure = settingFailure{}
		m.layout()
		return m, nil
	case "enter":
		return m.settingsCommitBuffer(row)
	}
	// Whatever Cmd the widget asks for is returned rather than dropped, exactly as the chat box
	// returns it (model.go) — a single-line field asks for none today (lineEditor.singleLine), and
	// swallowing one silently is how that stops being true unnoticed. The frame is NOT laid out
	// again: the field is one cell of one row, so nothing typed into it changes what the pane
	// measures.
	return m, m.settings.editor.editKey(msg)
}

// settingsCommitBuffer persists what was typed, and stays in the buffer when the binary will not have
// it. That is the whole reason a commit reports its outcome: a refused value is still on the screen,
// with the reason beside it (settingsNote), so the human fixes a port they mistyped instead of typing
// it again from nothing.
//
// The value is checked by the BINARY, not here (ADR 0011's thin renderer): what a key may hold is the
// registry's business and it is the write seam that asks — this pane knows only that a refusal means
// the file is unchanged. An EMPTY buffer commits nothing at all and simply closes, the /sessions
// empty-rename posture: ⏎ on a buffer the human has just cleared is far more likely to be an
// abandoned edit than a request to persist emptiness, and the deliberate way to take a value away is
// the reset backspace arms.
func (m Model) settingsCommitBuffer(row SettingRow) (tea.Model, tea.Cmd) {
	value := stripEscapes(strings.TrimSpace(m.settings.editor.value()))
	if value == "" {
		m.settings.kind, m.settings.editor = settingsKeyList, lineEditor{}
		m.layout()
		return m, nil
	}
	next, cmd, landed := m.settingsPersist(row, value)
	m = next
	if landed {
		m.settings.kind, m.settings.editor = settingsKeyList, lineEditor{}
	}
	m.layout()
	return m, cmd
}

// settingsTextValue is the prose a text row holds: what this pane last wrote for the key, else the
// value the run resolved ([SettingRow.Text] — the row's own cell carries a summary of it and nothing
// this can be seeded from). It is settingsPersistedValue for the one kind whose value is not what its
// row shows, and it is what the editor OPENS on, so an edit made twice in one session starts the
// second time from what the first one wrote.
//
// The trailing newline goes: a block scalar yields exactly one (the writer normalizes to it), and
// seeding a field with it would open the editor on a blank last line the human did not type and would
// have to delete to leave the prompt as it was.
func (m Model) settingsTextValue(row SettingRow) string {
	if edit, ok := m.settingEditOf(row.Path); ok {
		return edit.value
	}
	return strings.TrimRight(row.Text, "\n")
}

// newSettingsTextEditor is the field a text row's prose is written in: a [lineEditor] left MULTI-LINE
// — the one field in this package that keeps the widget's newline binding, because ⏎ here means what
// it means in any editor and the commit moves to ctrl+s (ADR 0037 decision 10).
//
// It is left at the widget's own width, which is what makes ↑/↓ walk the prompt's LOGICAL lines: the
// pane paints one line per line and wraps what does not fit (renderSettingsText), so a caret walking
// the widget's idea of visual rows would step through wraps the pane never drew.
func newSettingsTextEditor(shape tea.CursorShape, surface color.Color, seed string) lineEditor {
	e := newLineEditor(shape, surface)
	e.setValue(seed)
	return e
}

// settingsTextTarget is the row an open text editor is writing, and whether there still IS one — the
// settingsEnumTarget contract for the third second-step, and what keeps a ctrl+s from committing a
// prompt into whatever key moved into that index.
func (m Model) settingsTextTarget(rows []SettingRow) (SettingRow, bool) {
	if m.settings.kind != settingsTextEditor {
		return SettingRow{}, false
	}
	row, ok := m.settingsSelectedRow(rows)
	if !ok || !settingsWritable(row) {
		return SettingRow{}, false
	}
	return row, true
}

// settingsWritable reports whether a row is edited in the multi-line field: the one kind whose value
// is prose, and only where the registry lets this surface write it (settingsBufferable's twin).
func settingsWritable(row SettingRow) bool {
	return row.Editable && row.Kind == SettingText
}

// settingsTextKey routes a keypress in the multi-line field: ctrl+s commits, esc discards, and every
// other key goes to the FIELD — ⏎ among them, which is the whole difference between this step and the
// one-line buffer. What the human gets is what the chat box gives, over several lines.
//
// esc discarding rather than committing is deliberate and is what the legend says (settingsTextHint):
// the field holds a page of prose, and the key that walks away from an edit must not be the one that
// persists it.
func (m Model) settingsTextKey(msg tea.KeyPressMsg, row SettingRow) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// The refusal goes with the abandoned edit, the buffer's own reason: a ✗ left on the row would
		// report a failure against an edit nobody is making any more.
		m.settings.kind, m.settings.editor = settingsKeyList, lineEditor{}
		m.settings.failure = settingFailure{}
		m.layout()
		return m, nil
	case "ctrl+s":
		return m.settingsCommitText(row)
	}
	// The frame IS laid out again, unlike the one-line buffer's: this field is the pane's row list, so
	// a line added or removed changes how many rows the pane measures.
	cmd := m.settings.editor.editKey(msg)
	m.layout()
	return m, cmd
}

// settingsCommitText persists the prose and stays in the field when the binary will not have it — the
// edit buffer's contract, for its reason: a refused prompt is still on the screen with the reason
// beside it, so the human fixes the placeholder they mistyped instead of writing the prompt again.
//
// An EMPTY field commits nothing at all and simply closes, the buffer's own posture: a prompt cleared
// to nothing is far more likely to be an abandoned edit than a request to send no prompt, and the
// deliberate way to take the prompt away is the reset backspace arms (which the binary's own validator
// says in as many words).
func (m Model) settingsCommitText(row SettingRow) (tea.Model, tea.Cmd) {
	value := strings.TrimRight(stripEscapes(m.settings.editor.value()), "\n")
	if strings.TrimSpace(value) == "" {
		m.settings.kind, m.settings.editor = settingsKeyList, lineEditor{}
		m.layout()
		return m, nil
	}
	next, cmd, landed := m.settingsPersist(row, value)
	m = next
	if landed {
		m.settings.kind, m.settings.editor = settingsKeyList, lineEditor{}
	}
	m.layout()
	return m, cmd
}

// settingsArmReset arms the selected row's reset-to-default — backspace on a row that HAS something to
// reset. Arming is deliberately a state and not the act: removing a line from a file the human
// maintains by hand is not something a stray keypress does, so the hint line asks
// (settingsResetHint) and ⏎ answers.
//
// A row with nothing to reset arms nothing and says nothing: a key already at its default has no line
// to remove (settingsResettable), and a note about a no-op would be noise on a row the human is
// simply passing through.
func (m Model) settingsArmReset(rows []SettingRow) (tea.Model, tea.Cmd) {
	row, ok := m.settingsSelectedRow(rows)
	if !ok || !m.settingsResettable(row) {
		return m, nil
	}
	m.settings.kind = settingsResetArmed
	m.layout()
	return m, nil
}

// settingsResetKey answers the armed reset: ⏎ confirms it and esc cancels. Any other key leaves it
// armed, the sessionConfirmKey posture — a confirmation is not something a mistyped key should be able
// to dismiss quietly, and the hint line is still on the screen saying which two keys mean anything.
func (m Model) settingsResetKey(msg tea.KeyPressMsg, row SettingRow) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.settings.kind = settingsKeyList
		m.layout()
		return m, nil
	case "enter":
		return m.settingsReset(row)
	}
	return m, nil
}

// settingsReset returns one key to its default through [Options.ResetSetting] — the key's line REMOVED
// from the file rather than the default written into it (ADR 0035), so the value goes back to being
// described by the binary and documented by its commented example.
//
// The outcomes are settingsWrite's, for the same reasons: no seam wired, or refused, changes nothing
// but the row's own line; a reset that lands is recorded as the edit it is — the key now yields its
// DEFAULT — and that default is applied to the running session now, on the same terms a written
// value is (settingsApplied). The armed state ends either way: the question was answered.
func (m Model) settingsReset(row SettingRow) (tea.Model, tea.Cmd) {
	m.settings.kind = settingsKeyList
	if m.opts.ResetSetting == nil {
		m.settings.failure = settingFailure{path: row.Path, msg: noSettingsWriterNote}
		m.layout()
		return m, nil
	}
	if err := m.opts.ResetSetting(row.Path); err != nil {
		m.settings.failure = settingFailure{path: row.Path, msg: err.Error()}
		m.layout()
		return m, nil
	}
	m, cmd := m.settingsApplied(row, settingEdit{path: row.Path, value: row.Default, reset: true})
	m.layout()
	return m, cmd
}

// settingsWrite persists one key through [Options.WriteSetting] and records what became of it. It is
// synchronous, the SaveHostAcknowledgement posture: one small file, spliced and renamed, on a keypress
// the human is waiting on — a Cmd would only let the pane repaint a row whose value had not landed yet.
//
// Three outcomes, and each changes exactly one thing:
//
//   - no writer wired — the row says so and nothing else moves (the nil-seam degrade);
//   - refused — the row carries the error and the key is treated as UNCHANGED, because the file is
//     unchanged: no edit is recorded, no live apply, no marker;
//   - landed — the edit is recorded for the row's marker AND applied to the running session on the
//     same keypress (settingsApplied), so the session and the file agree the same instant.
func (m Model) settingsWrite(row SettingRow, value string) (tea.Model, tea.Cmd) {
	m, cmd, _ := m.settingsPersist(row, value)
	m.layout()
	return m, cmd
}

// settingsPersist is settingsWrite's body and its outcome: the model after the attempt, and whether the
// write LANDED. The bool exists for the edit buffer, which is the one caller whose next move depends on
// it — a refused value keeps its buffer open so it can be corrected (settingsCommitBuffer), where a
// refused toggle has nothing to keep. It does not lay out: the caller does, once, after it has finished
// deciding what the pane is now doing.
func (m Model) settingsPersist(row SettingRow, value string) (Model, tea.Cmd, bool) {
	if m.opts.WriteSetting == nil {
		m.settings.failure = settingFailure{path: row.Path, msg: noSettingsWriterNote}
		return m, nil, false
	}
	if err := m.opts.WriteSetting(row.Path, value); err != nil {
		m.settings.failure = settingFailure{path: row.Path, msg: err.Error()}
		return m, nil, false
	}
	m, cmd := m.settingsApplied(row, settingEdit{path: row.Path, value: value})
	return m, cmd, true
}

// settingsApplied records an edit that LANDED and puts it into effect — the one place both halves of
// "the file now says this" happen, so a write and a reset cannot drift apart on either (a reset of
// `mode` must switch the running session's mode exactly as a write of it does).
//
// The apply is the third step of ADR 0037's validate → persist → apply, and it runs AFTER the write
// has landed: the file already expresses what the human asked for, so an apply that fails does not
// unwind it. The edit stays recorded, the row carries the failure instead (settingsApplyFailedNote),
// and a re-committed edit retries the apply against the same persisted value.
//
// The apply is guarded on a non-empty value as well as on the key: a reset records the key's
// DEFAULT, and a key whose default is unset would otherwise hand the seam an empty string to
// resolve — which is a question about a key the file no longer sets, not a value it now holds.
//
// The Cmd it hands back is the apply's own, and today exactly one key produces one: a colour-scheme
// switch rebuilds every style under a screen already painted in the previous palette, so the frame
// has to be cleared rather than redrawn over (settingsApplyLocal). Every other key returns nil and
// every caller passes whatever comes back on unchanged.
func (m Model) settingsApplied(row SettingRow, edit settingEdit) (Model, tea.Cmd) {
	var applyErr error
	var cmd tea.Cmd
	if edit.value != "" {
		m, edit.note, cmd, applyErr = m.settingsApplyLive(row.Path, edit.value)
	}
	m = m.recordSettingEdit(edit)
	if applyErr != nil {
		m.settings.failure = settingFailure{path: row.Path, msg: settingsApplyFailedNote + applyErr.Error()}
	}
	return m, cmd
}

// settingsApplyLive puts one persisted key into effect and reports what the row has to say about it:
// the boundary note (empty for a key that is simply in force now) and the refusal of an apply that
// could not happen. Two classes of key and no third:
//
//   - the keys whose whole effect is on THIS screen (settingsApplyLocal) are applied here, because
//     there is no engine on the other side of them to ask;
//   - every other key goes out through [Options.ApplySetting], the binary's dispatcher, which owns
//     the schema and therefore is the only thing that can turn the file's spelling of a value into
//     whatever the engine seam behind it takes (ADR 0037 decision 2).
//
// `mode` is the one key with a foot in both: the seam moves the Agent, and the footer renders the
// mode from opts.Mode — so the mirror Shift+Tab keeps in step is updated here too, but only once the
// apply has LANDED, or the footer would report an autonomy the engine is not running.
// A local apply may also hand back a Cmd and a note of its own, which is why the local branch no
// longer returns an empty note: a colour-scheme switch that loaded with warnings says so on the row
// (settingsApplyLocal) through the same slot "applies at next clear" uses, and asks for the repaint
// its new palette needs. The seam's own keys are unchanged — [Options.ApplySetting] returns a note
// and never a Cmd, because what it moves is on the far side of the renderer.
func (m Model) settingsApplyLive(path, value string) (Model, string, tea.Cmd, error) {
	if applied, note, cmd, ok, err := m.settingsApplyLocal(path, value); ok {
		return applied, note, cmd, err
	}
	if m.opts.ApplySetting == nil {
		return m, "", nil, nil // no live apply wired: the write stands on its own (ADR 0031's nil-seam degrade)
	}
	note, err := m.opts.ApplySetting(path, value)
	if err != nil {
		return m, "", nil, err
	}
	if path == settingKeyMode {
		m.opts.Mode = domain.Mode(value) // the footer renders the mode from opts.Mode (footerContent)
	}
	return m, note, nil, nil
}

// settingsApplyLocal applies the keys the RENDERER itself owns — the ones whose entire effect is a
// field on this Model — and reports whether the key was one of them. They are named rather than
// derived because what makes a key local is that nothing behind [Options.ApplySetting] would have
// anything to do with it: routing them out to the binary and back would only give the pane a longer
// way to reach its own state.
//
// A value the renderer's own vocabulary does not know is returned as an apply error rather than
// silently ignored. The binary validates before it writes, so this cannot happen through the pane —
// but the pane is not the only thing that can put a value in the file, and a spinner style this
// build has no animation for is worth a sentence on the row.
//
// Two of these keys have more to say than "done": the note is the row's own sentence about the apply
// (empty for a key that simply took effect), and the Cmd is what the apply needs the program to do
// next. Only the colour scheme uses either.
func (m Model) settingsApplyLocal(path, value string) (Model, string, tea.Cmd, bool, error) {
	switch path {
	case settingKeyAutoTitle:
		m.opts.AutoTitle = value == settingTrue
	case settingKeyShowScrollbar:
		// The config key is positive and the option is inverted (the polarity flips in cmd/apogee).
		// The bar's gutter column is transcript width, so the frame is laid out again from here
		// rather than left to the next resize.
		m.opts.HideScrollbar = value != settingTrue
		m.layout()
	case settingKeySpinner:
		style, err := ParseSpinnerStyle(value)
		if err != nil {
			return m, "", nil, true, err
		}
		// Both halves: the option is the record of what is selected, m.spin is what paints. The
		// frame counter is left where it is — every style's glyph indexes it modulo its own frame
		// count — so a style swapped mid-run continues the animation instead of restarting it.
		m.opts.Spinner, m.spin.style = style, style
	case settingKeySpinnerColor:
		on := value == settingTrue
		m.opts.SpinnerColor, m.spin.color = on, on
	case settingKeyColorScheme:
		note, cmd, err := m.applyColorScheme(value)
		return m, note, cmd, true, err
	case settingKeyCursorShape:
		shape, err := ParseCursorShape(value)
		if err != nil {
			return m, "", nil, true, err
		}
		// steadyCursor is idempotent: it restates the retired virtual cursor and the styles the real
		// terminal cursor is drawn from, which is the whole of what the shape changes.
		m.opts.CursorShape = shape
		steadyCursor(&m.input, shape)
	default:
		return m, "", nil, false, nil
	}
	return m, "", nil, true, nil
}

// applyColorScheme puts a named colour scheme into effect on THIS screen — the live half of ADR
// 0040's settings picker, and the one local apply that rebuilds the whole look rather than moving a
// field.
//
// It re-RESOLVES rather than reading a palette off the Options, so a scheme file the human has just
// edited lands on the next switch: the seam reads the folder every time it is asked
// ([Options.ResolveScheme]), which is the whole of what apogee offers instead of watching the file.
// The load is forgiving, so a resolve that warned still produces a usable palette — the warnings
// become transcript notes (design call 11) and the row says how many, through the same slot a
// boundary note uses.
//
// Four things move, and each for its own reason:
//
//   - the theme is rebuilt, which is what a scheme IS ([newTheme]);
//   - the prompt textarea is re-filled, because its four background slots belong to a Bubble Tea
//     widget the theme cannot reach from a style (fillInput) — the same posture steadyCursor takes
//     for the caret;
//   - the block paint cache is cleared, because every memoised paint in it is in the previous
//     palette and its key does not name the theme (paintcache.go);
//   - the Options' own record of the scheme is updated, so a report that names the scheme in force
//     names this one.
//
// The Cmd is tea.ClearScreen: the terminal still holds the previous palette's scrollback and
// backgrounds outside the frame apogee repaints, so the screen is cleared and drawn again whole.
func (m *Model) applyColorScheme(name string) (string, tea.Cmd, error) {
	if m.opts.ResolveScheme == nil {
		return "", nil, errNoSchemeResolver
	}
	s, warnings := m.opts.ResolveScheme(name)
	m.th = newTheme(s)
	fillInput(&m.input, m.th.surface)
	m.transcript.paints.clear()
	m.opts.ColorScheme, m.opts.ColorSchemeName = s, name
	for _, w := range warnings {
		m.transcript.addEphemeralNote(w)
	}
	m.layout()
	return colorSchemeWarningNote(len(warnings)), tea.ClearScreen, nil
}

// errNoSchemeResolver is what an unwired [Options.ResolveScheme] costs: the key is persisted and the
// row says the switch could not happen now, which is the honest sentence — the scheme IS in the file
// and the next start will be drawn in it.
var errNoSchemeResolver = errors.New("no colour-scheme resolver is wired; the new scheme applies at the next start")

// colorSchemeWarningNote is the row's sentence for a switch that loaded with complaints, and "" for
// the ordinary one that did not. The warnings themselves are in the transcript — this only says how
// many, because the pane is drawn OVER that transcript and a human answering the picker would
// otherwise see nothing at all.
func colorSchemeWarningNote(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return "applied with 1 warning"
	default:
		return "applied with " + strconv.Itoa(n) + " warnings"
	}
}

// The registry paths this package names. settingKeyMode is the one key the pane MIRRORS after the
// seam applied it (the footer's own copy); the rest are the renderer-owned keys settingsApplyLocal
// puts into effect itself. Every other key is a path this package never spells — the binary's
// dispatcher routes them by name, which is exactly the coupling ADR 0037 decision 2 keeps out here.
const (
	settingKeyMode          = "mode"
	settingKeyAutoTitle     = "auto-title"
	settingKeyShowScrollbar = "ui.show-scrollbar"
	settingKeySpinner       = "ui.spinner"
	settingKeySpinnerColor  = "ui.spinner-color"
	settingKeyColorScheme   = "ui.color-scheme"
	settingKeyCursorShape   = "cursor-shape"
)

// settingsApplyFailedNote opens the row's failure when the WRITE landed and the apply did not: the
// file has the new value and the session does not, which is a different sentence from a refused
// write and has to read like one (ADR 0037 decision 1).
const settingsApplyFailedNote = "saved — live apply failed: "

// recordSettingEdit returns the Model with edit recorded in the session's journal, replacing any
// earlier edit of the same key — the last one is what the file says, whether it wrote a value or
// removed the line. The slice is built FRESH rather than appended to, the value-copied Model's rule
// (ADR 0011, doc.go): an append could write into an array a Model copy still in flight is sharing, and
// the copies are not ours to reason about.
//
// A landed write also clears the pane's failure and answer slots, which are one attempt's outcome and
// not one row's condition: the human just saw a write succeed, and a refusal — or a confirmation that
// nothing had changed — left over from a previous keypress would go on contradicting it.
func (m Model) recordSettingEdit(edit settingEdit) Model {
	next := make([]settingEdit, 0, len(m.settingEdits)+1)
	for _, e := range m.settingEdits {
		if e.path != edit.path {
			next = append(next, e)
		}
	}
	m.settingEdits = append(next, edit)
	m.settings.failure = settingFailure{}
	m.settings.answer = settingAnswer{}
	return m
}

// settingEditOf is what this session did to path through the settings surface, and whether it did
// anything at all. A linear scan over at most one edit per config key is the right shape here: the list
// is short, it is read once per row per frame, and a map would be a reference the Model's copies would
// share.
func (m Model) settingEditOf(path string) (settingEdit, bool) {
	for _, e := range m.settingEdits {
		if e.path == path {
			return e, true
		}
	}
	return settingEdit{}, false
}

// toggledSetting is the value ⏎ writes for a bool row: the other one. A row whose value is neither
// (an unset key with no default) toggles ON, because the human pressed ⏎ to change something and
// "false" is what an absent bool key already resolves to.
func toggledSetting(value string) string {
	if value == settingTrue {
		return settingFalse
	}
	return settingTrue
}

// indexOfSetting is where value sits in a closed vocabulary, or −1 when the vocabulary does not
// contain it — a key the user has set to something the registry does not offer, which the sub-list
// answers by opening at its first row rather than by refusing to open.
func indexOfSetting(values []string, value string) int {
	for i, v := range values {
		if v == value {
			return i
		}
	}
	return -1
}

// settingsPersistedValue is what the pane believes the FILE now holds for a row: the value it wrote
// itself if it wrote one, else the value this run resolved. It is the base every edit starts from, so
// two ⏎ presses on a bool return it to where it was and a sub-list re-opened after an edit opens on
// the value that edit set — rather than both starting again from a resolution that is now behind the file.
func (m Model) settingsPersistedValue(row SettingRow) string {
	if edit, ok := m.settingEditOf(row.Path); ok {
		return edit.value
	}
	return row.Value
}

// settingsEnumTarget is the row an open value sub-list is asking about, and whether there still IS
// one. The sub-list is a view of the SELECTED row and the rows are re-derived under it, so a selection
// that no longer names an editable enum — a list that shrank, a row whose kind changed — has nothing
// to ask about, and both the key router and the renderer fall back to the key list on the same
// predicate rather than each guessing.
func (m Model) settingsEnumTarget(rows []SettingRow) (SettingRow, bool) {
	if m.settings.kind != settingsEnumList {
		return SettingRow{}, false
	}
	sel := m.settingsSelection(len(rows))
	if sel < 0 {
		return SettingRow{}, false
	}
	row := rows[sel]
	if !settingsPickable(row) || len(m.settingsVocabulary(row)) == 0 {
		return SettingRow{}, false
	}
	return row, true
}

// settingsPickable reports whether a row is edited in the value SUB-LIST: the two kinds that answer a
// ⏎ with a closed list of values, and only where the registry lets this surface act on the key at all.
func settingsPickable(row SettingRow) bool {
	return row.Editable && (row.Kind == SettingEnum || row.Kind == SettingServer)
}

// settingsVocabulary is the list a row's sub-list offers, however that row comes by one: the
// registry's own [SettingRow.EnumValues] for an enum, and the switchable Upstreams for the `server`
// row, whose vocabulary is what THIS config's `servers:` block names and therefore cannot live in a
// static table ([SettingServer]).
//
// The colour-scheme row is the `server` row's twin here and diverges the same way: its values are the
// built-ins plus whatever `*.yaml` files the human's schemes folder holds right now ([Options.ListSchemes]),
// which no static table can name either. It reaches the pane as an ordinary enum (cmd/apogee's
// settingKind) because picking one is picking a value from a list — only where the list COMES from
// differs — so it is matched on its path rather than on a kind of its own.
//
// Every step of the sub-list asks this — the open, the walk, the accept, the paint — so a list that
// changed under an open question is one list wherever it is read, and the accept can only ever take a
// value the frame the human answered was showing.
func (m Model) settingsVocabulary(row SettingRow) []string {
	if row.Path == settingKeyColorScheme {
		if m.opts.ListSchemes == nil {
			return nil // unwired: the row opens nothing rather than offering an empty list
		}
		return m.opts.ListSchemes()
	}
	if row.Kind != SettingServer {
		return row.EnumValues
	}
	servers := m.servers()
	names := make([]string, 0, len(servers))
	for _, choice := range servers {
		names = append(names, choice.Name)
	}
	return names
}

// settingsCurrentValue is the value a sub-list opens on and marks "(current)": what the pane believes
// the file holds (settingsPersistedValue) for every key but one.
//
// The `server` row is that one, and its honest answer is the server the session is ON — identified by
// endpoint, the picker's own comparison — rather than the entry the key names: `/server` moves a
// session and rewrites the key without this pane ever hearing about it, so a value read off the
// launch resolution would mark the server the session has left.
func (m Model) settingsCurrentValue(row SettingRow) string {
	if row.Kind == SettingServer {
		for _, choice := range m.servers() {
			if choice.Endpoint == m.opts.Endpoint {
				return choice.Name
			}
		}
	}
	return m.settingsPersistedValue(row)
}

// settingsSelectedRow is the highlighted row, and whether there is one — the read every second step
// starts from. It exists so the buffer and the reset ask the same question of the same clamp the enum
// sub-list does, rather than each indexing the slice on its own arithmetic.
func (m Model) settingsSelectedRow(rows []SettingRow) (SettingRow, bool) {
	sel := m.settingsSelection(len(rows))
	if sel < 0 {
		return SettingRow{}, false
	}
	return rows[sel], true
}

// settingsBufferTarget is the row an open edit buffer is typing into, and whether there still IS one —
// the settingsEnumTarget contract for the other second step, and the predicate that keeps a ⏎ from
// committing a buffer into whatever key moved into that index.
func (m Model) settingsBufferTarget(rows []SettingRow) (SettingRow, bool) {
	if m.settings.kind != settingsValueBuffer {
		return SettingRow{}, false
	}
	row, ok := m.settingsSelectedRow(rows)
	if !ok || !settingsBufferable(row) {
		return SettingRow{}, false
	}
	return row, true
}

// settingsResetTarget is the row an armed reset is about, and whether it is still there and still worth
// resetting. Re-asked on the confirming keypress rather than trusted from the arming one, so a ⏎ can
// never remove a line from a key the human did not arm.
func (m Model) settingsResetTarget(rows []SettingRow) (SettingRow, bool) {
	if m.settings.kind != settingsResetArmed {
		return SettingRow{}, false
	}
	row, ok := m.settingsSelectedRow(rows)
	if !ok || !m.settingsResettable(row) {
		return SettingRow{}, false
	}
	return row, true
}

// settingsBufferable reports whether a row is edited in the caret buffer: the two kinds with no closed
// vocabulary and more than two values, and only where the registry lets this surface write at all.
func settingsBufferable(row SettingRow) bool {
	return row.Editable && (row.Kind == SettingString || row.Kind == SettingInt)
}

// settingsResettable reports whether a row has anything for a reset to DO. Two ways it can: this pane
// wrote the key (so the file carries a line the pane put there), or the value the run resolved differs
// from the built-in default — the renderer's honest read of "the file, an environment variable or a
// flag is setting this", since a key nothing sets resolves to its default by definition.
//
// A row already showing its default therefore arms nothing. That is not a shortcut: reset means "remove
// the line", and for such a row there is either no line to remove or removing it changes nothing the
// human can see — so the keypress is better as a no-op than as a confirmation prompt for a no-op. An
// overridden row is still resettable: the FILE may well set it, and the row's own note says the
// override outranks what the file says.
//
// A kind that takes no reset AT ALL is refused before either question is asked (settingsResetKind).
func (m Model) settingsResettable(row SettingRow) bool {
	if !row.Editable || !settingsResetKind(row) {
		return false
	}
	if _, edited := m.settingEditOf(row.Path); edited {
		return true
	}
	return row.Value != row.Default
}

// settingsResetKind reports whether a row's KIND takes a reset at all. Every kind does but one: the
// `server` row ([SettingServer]), where backspace is inert.
//
// Its value is not a value this pane writes. `server:` is the RECORDING of a switch the session
// performed — written by the switch seam itself, which is this key's entire persistence (ADR 0036
// decision 2) — and the row is a second entrance to that seam and nothing else (ADR 0037 decision 5,
// ratified call 4). A reset would be a second door into the key that performs no rehome: it would
// splice the line away while the session went on running against the server it named, so the file and
// the wire would disagree with nothing journaled and nothing said. That is precisely the "second,
// less-informed flow" decision 5 refuses.
//
// Nor is there anything to reset TO. A reset REMOVES the line rather than freezing today's default
// (ADR 0035, restated by ADR 0037 decision 9), and this key's default is unset — which is not a server
// a session can run against but the state ADR 0036 decision 3 answers by ASKING at the next launch.
// Rendering that as the `default *` a reset shows (decision 8) would have the row claim the session is
// on nothing while the heartbeat says otherwise. The alternative shape — reset meaning "switch to some
// configured default and record it" — is the freezing the reset contract forbids, and it invents a
// default the schema does not have.
//
// So the row simply has no reset, the hint line stops advertising one on it (settingsPaneHint), and
// the way to change this key stays the one door it has: choose a server, and the session moves.
func settingsResetKind(row SettingRow) bool {
	return row.Kind != SettingServer
}

// clampSelection keeps selected inside a row list that moved under the open pane. An empty list pins
// it at zero, which renderSettings paints with no highlight at all (the picker's own contract).
func (p *settingsPane) clampSelection(n int) {
	switch {
	case n == 0:
		p.selected = 0
	case p.selected >= n:
		p.selected = n - 1
	case p.selected < 0:
		p.selected = 0
	}
}

// settingsSelection is which SETTING row is highlighted, clamped to a list of n rows: −1 when there
// are none, which is renderPopup's own "no highlight" convention. It is the one place the stored
// index is turned into an index anything may be read by, so a stale selection cannot reach a slice.
func (m Model) settingsSelection(n int) int {
	if n == 0 {
		return -1
	}
	return clampInt(m.settings.selected, 0, n-1)
}

// ----------------------------------------------------------------------------
// Rendering
// ----------------------------------------------------------------------------

// settingsDisplay is the pane's row list as the popup module takes it — the setting rows with their
// section headers and the spacers above them interleaved — what each of those rows IS to the painter
// (popupRowKind), and where the selected KEY landed among them. The three travel together because
// they are one composition: inserting a header shifts every row after it, so a selection index or a
// kind list derived separately would decorate the wrong row the moment a section opened above it.
type settingsDisplay struct {
	rows     []popupRow
	kinds    []popupRowKind // parallel to rows: which are section headers, which is being edited
	keys     []int          // parallel to rows: which SETTING row each one shows; −1 for a header or a spacer
	selected int            // index into rows of the selected key row; −1 when nothing is highlighted
}

// settingsDisplayRows composes the FULL row list the pane paints: a blank spacer and a single-cell
// header row wherever the section changes, then one four-cell row per key. Rows arrive in the
// provider's order, which is the config template's order (ADR 0035), so the pane reads in the order
// the file does and a key added to the registry appears where it was added.
//
// Headers and spacers are rows the popup module paints and NOT rows the selection can land on. That
// is why the selection indexes the setting rows and this method reports where the highlighted one
// ended up: ↑/↓ walks keys, the labels scroll past with them, and no keypress can leave the ❯ on a
// label there is nothing to do with. The keys list says the same thing for every display row rather
// than for the selected one alone — which is what a POINTER needs, since a click can land on any row
// of the list and has to be told the ones that are labels (settingsPaint.rowAt, mouse.go).
//
// The spacer is what makes the sections read as sections (docs/layout/settings-screen-layout.md) —
// a header flush against the last key of the section above it divides nothing — and the FIRST
// header is the one that goes without: the description header above it already closes with a blank
// line of its own (settingsBody), and two blanks would open the pane on a gap.
func (m Model) settingsDisplayRows(rows []SettingRow, selected int) settingsDisplay {
	if len(rows) == 0 {
		return settingsDisplay{rows: singleCellRows([]string{noSettingsRow}), selected: -1}
	}
	out := make([]popupRow, 0, len(rows)+len(rows)/2) //nolint:mnd // a header and a spacer every few keys, at a guess
	kinds := make([]popupRowKind, 0, cap(out))
	keys := make([]int, 0, cap(out))
	add := func(row popupRow, kind popupRowKind, key int) {
		out, kinds, keys = append(out, row), append(kinds, kind), append(keys, key)
	}
	display := -1
	section := ""
	for i, row := range rows {
		if row.Section != "" && row.Section != section {
			if len(out) > 0 {
				add(popupRow{""}, popupRowPlain, -1) // the spacer, blank in every column
			}
			section = row.Section
			add(popupRow{stripEscapes(row.Section)}, popupRowHeading, -1)
		}
		if i == selected {
			display = len(out)
		}
		cells, kind := m.settingRowCells(row), popupRowPlain
		// An open edit buffer replaces the SELECTED row's value cell, in place: the key stays where it
		// was in the list and in its column, so the human types into the row they chose rather than
		// into a prompt that has covered it (the /sessions rename idiom, one column narrower — a
		// setting's row is a key and a value, and it is the value being typed).
		if i == selected && m.settingsEditing(row) {
			if m.settings.kind == settingsValueBuffer {
				cells = m.settingBufferCells(row)
			}
			kind = popupRowEditing
		}
		add(cells, kind, i)
	}
	return settingsDisplay{rows: out, kinds: kinds, keys: keys, selected: display}
}

// settingKeyAt is the SETTING row display row i shows, and whether it shows one at all: a section
// label and the spacer above it show none. It is the one read of the keys list, so a display index
// from anywhere — a mouse click, a test — cannot index it out of range.
func (d settingsDisplay) settingKeyAt(i int) (int, bool) {
	if i < 0 || i >= len(d.keys) || d.keys[i] < 0 {
		return 0, false
	}
	return d.keys[i], true
}

// settingsEditing reports whether the pane is EDITING the given row — the state the row is painted
// in the edit tone for (popupRowEditing, docs/layout/settings-screen-layout.md requirement 2). Every
// second step that holds ONE row counts, because for the human they are the same fact: the buffer,
// which is typed into on the row itself, the enum sub-list, which is answering about it, and the
// multi-line field, which is writing its value. The last two draw a pane of their own
// (renderSettingsEnum, renderSettingsText) rather than a list with one row lit, so what this reports
// there is the pane's STATE rather than a row currently on the screen — the predicate is about which
// row is being edited, not about which renderer is up.
//
// The kind's own conditions are re-asked here (settingsBufferable, the enum's vocabulary) for the
// reason the two targets re-ask them: the rows are re-derived under an open step, and a row that can
// no longer hold the step it opened is not being edited whatever the pane's field still says.
func (m Model) settingsEditing(row SettingRow) bool {
	switch m.settings.kind {
	case settingsValueBuffer:
		return settingsBufferable(row)
	case settingsEnumList:
		return settingsPickable(row) && len(m.settingsVocabulary(row)) > 0
	case settingsTextEditor:
		return settingsWritable(row)
	}
	return false
}

// settingBufferCells is the row being typed into: the same four columns, with the FIELD where the
// value goes — its text with the caret drawn in at the position the next keystroke will land
// (lineEditor.textWithCaret). The value the key HOLDS is deliberately not shown beside it — the field
// opened seeded with it (settingsBufferSeed), so what is on the row is what will be written, and a
// second copy of the old value would only make the human wonder which one the ⏎ takes.
//
// The field's text is escape-stripped here like every other cell: it is keystrokes, and a paste is
// keystrokes too — a bracketed paste carrying an OSC 8 opener would otherwise reach the popup module,
// which strips nothing (doc.go). The caret glyph is added before the strip because it is this
// package's own and has nothing to strip.
func (m Model) settingBufferCells(row SettingRow) popupRow {
	return popupRow{
		stripEscapes(row.Path),
		m.settingsEditText(),
		stripEscapes(settingsSourceMarker(row.Source)),
		stripEscapes(m.settingsNote(row)),
	}
}

// settingsEditText is what the edit row's value cell HOLDS: the field's text with the caret glyph
// drawn in where the next keystroke lands (lineEditor.textWithCaret), escape-stripped like every
// other cell handed the popup module (doc.go).
//
// It is a derivation of its own because the mouse reads it too: a click's column is turned into a
// rune offset by measuring these very cells, and the highlight measures them back the other way
// (mouse.go). One string for the painter and the pointer, so the glyph the human clicks on is the
// glyph the caret lands at.
func (m Model) settingsEditText() string {
	return stripEscapes(m.settings.editor.textWithCaret(settingsCaret))
}

// settingsPaneHint is the legend at the pane's foot: one per step, because the keys mean different
// things in each — and the armed reset's is the one that ASKS, which is what makes backspace safe to
// give a destructive act (settingsResetHint). The enum sub-list has its own renderer and its own hint.
//
// In the key list the legend is the SELECTED row's: the one row this pane never resets drops the key
// that would do nothing on it (settingsResetKind), and rows is passed rather than re-derived so the
// legend is about the row the frame is highlighting.
func (m Model) settingsPaneHint(rows []SettingRow) string {
	switch m.settings.kind {
	case settingsValueBuffer:
		return settingsBufferHint
	case settingsResetArmed:
		return settingsResetHint
	case settingsTextEditor:
		return settingsTextHint
	}
	if row, ok := m.settingsSelectedRow(rows); ok && !settingsResetKind(row) {
		return settingsNoResetHint
	}
	return settingsHint
}

// settingRowCells is one key's row in the pane's fixed column schema — ["key", "value", "(env)",
// "· use /confine"] — so the values line up in one column however long the keys beside them
// run, the override marks in the next, and the last carries whatever else is true of the row. A tier
// a key does not state is an EMPTY cell, which still pads, so a key with no override cannot slide the
// note of the row under it sideways; a tier NO key states collapses away entirely (layoutPopupRow),
// which is what keeps a config nothing overrides from paying for a marker column.
//
// The last cell is one column and not two on purpose: a row either cannot be written here (its
// pointer) or can and this pane wrote it (its marker) — never both — so one column says whichever is
// true and a config with no read-only rows and no edits pays for neither.
//
// Every cell is escape-stripped here, at the producer, as the popup module's contract requires
// (doc.go): a value is the CONFIG FILE's text — a server name, a present command the human typed —
// and a file on this machine is no more trusted than a model's reply. The mask, the structured
// summary and the pointer wording all arrived decided ([SettingRow]); nothing here
// reformats a value or rules on what may be edited.
func (m Model) settingRowCells(row SettingRow) popupRow {
	return popupRow{
		stripEscapes(row.Path),
		stripEscapes(m.settingsValueCell(row)),
		stripEscapes(settingsSourceMarker(row.Source)),
		stripEscapes(m.settingsNote(row)),
	}
}

// settingsValueCell is the value column: what the SESSION is running for this key. That is the value
// the provider resolved, until this pane changes it — every edit and every reset applies on the ⏎
// that persists it (ADR 0037 decision 1), so what this pane wrote IS what the session runs, while the
// provider still answers with the resolution this run started from. Such a row carries the ` *` that
// says this session changed it (settingsEditMarker).
func (m Model) settingsValueCell(row SettingRow) string {
	edit, ok := m.settingEditOf(row.Path)
	if !ok {
		return row.Value
	}
	return settingsEditedValue(row, edit) + settingsEditMarker
}

// settingsEditedValue is what an edited row shows in its value column: what this pane wrote, or —
// after a reset — the default the key went back to, which for a key that defaults to nothing is the
// word for nothing rather than a blank the marker would float after.
//
// A TEXT row keeps a summary of what was written rather than the prose itself, which is what its cell
// showed before the edit and all a row has room for — the count is re-derived here because the value
// the pane wrote is newer than the one the provider resolved (settingsTextSummary).
//
// A masked row keeps the MASK it arrived with: the pane persisted a secret it never held and has
// nothing to show for it ([SettingRow]), so the marker beside the mask is the whole of what such a
// row says about having been written. A RESET of it is not a secret at all — a removed line left
// nothing to keep quiet about — so it reads like every other emptied key.
func settingsEditedValue(row SettingRow, edit settingEdit) string {
	switch {
	case edit.reset && row.Default == "":
		return settingsUnsetValue
	case edit.reset:
		return row.Default
	case row.Kind == SettingText:
		return settingsTextSummary(edit.value)
	case row.Masked:
		return row.Value
	case edit.value == "":
		return settingsUnsetValue
	}
	return edit.value
}

// settingsTextSummary is how many lines a text value holds — the whole of what a ROW says about prose,
// since a row is one line and a system prompt is a page of them. It is the pane's own spelling of the
// summary the binary projects into [SettingRow.Value] (settingsrows.go), and it exists because the two
// are needed at different times: the projection answers from the resolution the run started with, and
// this answers for a value the pane wrote a moment ago, which no provider has heard about.
func settingsTextSummary(text string) string {
	value := strings.TrimSuffix(text, "\n")
	if value == "" {
		return ""
	}
	if n := strings.Count(value, "\n") + 1; n > 1 {
		return strconv.Itoa(n) + " lines"
	}
	return "1 line"
}

// settingsNote is the row's last cell: what became of this pane's own writes to the key, else where a
// key it will not write IS edited. In precedence order, because each one outranks what it replaces:
//
//   - a refused write — or a write that landed on a key whose live apply then failed — because the
//     human's last act on this row failed and nothing else about the row matters as much;
//   - the answer to an act that landed and changed nothing ("· already on macStudio"), which is the
//     only thing this row has to show for a keypress that moved neither the file nor the session
//     ([settingAnswer]) — and which the value cell, showing what it showed before, cannot say;
//   - the apply's own boundary note for an edit that landed at a boundary rather than at once
//     ("· applies at next clear"), which is the only deferral wording this surface has;
//   - on a row an environment variable or a flag is overriding, that the override will win again at
//     the next start (ADR 0037 decision 4). The edit itself DID apply — a pane edit outranks an
//     override for the running session — so the sentence is about precedence at the next start and
//     not about the edit having failed to land;
//   - nothing at all for every other edit, because settingsValueCell already shows what was written
//     and its ` *` already says this session wrote it; and
//   - the read-only row's pointer, which is the registry's own fact and the only one of these a pane
//     that has written nothing ever paints.
//
// A masked key has no note of its own: the marker on its still-masked value cell is the whole of what
// a row says about a secret it persisted and does not hold (settingsEditedValue).
func (m Model) settingsNote(row SettingRow) string {
	if m.settings.failure.path == row.Path && m.settings.failure.msg != "" {
		return "✗ " + m.settings.failure.msg
	}
	if m.settings.answer.path == row.Path && m.settings.answer.msg != "" {
		return "· " + m.settings.answer.msg
	}
	edit, edited := m.settingEditOf(row.Path)
	switch {
	case edited && edit.note != "":
		return "· " + edit.note // applied, at a boundary this session will cross (ADR 0037 decision 3)
	case edited && row.Source != SettingFromFile:
		return "· " + settingsSourceLabel(row) + " outranks at next launch"
	case edited:
		return "" // applied live: the value cell and its marker say it
	case row.EditPointer != "":
		return "· " + row.EditPointer
	}
	return ""
}

// settingsSourceLabel names the source that beat the file for a row — "APOGEE_MODE", "--mode" — for
// the override note to point at. A row that carries a source but no name for it falls back to the kind
// of source it was, so the sentence still says something true rather than trailing off.
func settingsSourceLabel(row SettingRow) string {
	if row.SourceName != "" {
		return row.SourceName
	}
	return "the " + string(row.Source)
}

// settingsBody is the pane's DESCRIPTION HEADER: the "Description:" label, what the SELECTED key is
// for ([SettingRow.Desc]) beside it, and one blank line closing the region off from the key list
// under it (docs/layout/settings-screen-layout.md). It is the difference between a list of key names
// and a list the human can act on — a registry description is where a key says what setting it
// actually changes, and the pane has one row's worth of room to say it in.
//
// The region is a FIXED height (ADR 0037 decision 9) — settingsDescLines lines of description
// whatever the description measures, plus the blank — and that is the whole point of composing it
// here rather than handing the module a string to wrap: the header of a one-line description pads
// out to the same height as the header of a three-line one, so walking the list with ↑/↓ moves the
// highlight and nothing else. A list that re-flowed under every keypress would be a list nobody can
// read down.
//
// The overflow is an ELLIPSIS on the last line rather than the module's "… (+N more lines)" marker:
// the marker is honest about a body block whose tail the human may want, and a caption on the row
// under the cursor is not that — it would spend one of the two lines saying that there was a third.
// Truncation is in the painter's own measure (truncateToWidth, ADR 0030).
//
// With no row selected there is no header at all: the pane is then showing its own empty-list row
// (noSettingsRow), and a label over nothing would describe nothing.
func (m Model) settingsBody(rows []SettingRow) string {
	sel := m.settingsSelection(len(rows))
	if sel < 0 {
		return ""
	}
	inner := popupInnerWidth(m.th, m.width)
	lead := settingsDescLabel + " " + stripEscapes(rows[sel].Desc)
	lines := wrapText(m.th, strings.TrimRight(lead, " "), inner)
	if len(lines) > settingsDescLines {
		// What did not fit is folded back onto the last line the region has and elided there, so the
		// cut is stated where the text stops rather than by a line the region has not got.
		rest := strings.Join(lines[settingsDescLines-1:], " ")
		lines = append(lines[:settingsDescLines-1], truncateToWidth(m.th, rest, inner))
	}
	for len(lines) < settingsDescLines {
		lines = append(lines, "")
	}
	// The blank line is the region's own: it is what sets the header off the list rather than
	// something the first section header has to pay for a second time (settingsDisplayRows).
	return strings.Join(append(lines, ""), "\n")
}

// renderSettings paints the open pane through the shared popup module: a titled, bordered pane
// spanning the full window width (m.width, flush with the input box below), the selected key's
// description as its header, the key rows under their spaced section labels, and the key legend. It returns
// "" when the pane is closed or when the frame cannot seat it, so View treats it exactly like the
// picker's slot.
//
// The row window is the SCREEN's to grant and there is no taste cap above it: this pane asks for
// every row it has and popupBudget answers with what the frame allotted it, which for this pane is
// the whole transcript budget (frameRowPlan). That is the one thing that differs from the picker's
// eight-row window — the browser and the picker cap themselves because they crowd a conversation
// they are read BESIDE, and this pane is read INSTEAD of one.
func (m Model) renderSettings() string {
	if !m.settings.open {
		return ""
	}
	rows := m.settingRows()
	if row, ok := m.settingsEnumTarget(rows); ok {
		return m.renderSettingsEnum(row)
	}
	if _, ok := m.settingsTextTarget(rows); ok {
		return m.renderSettingsText(rows)
	}
	spec, display, ok := m.settingsKeyListSpec(rows)
	if !ok {
		return "" // the frame cannot seat this pane (settingsGiveWayNote says so on the status line)
	}
	view, place := renderPopupPlaced(m.th, spec, m.width)
	// The drag-selection is overlaid on the COMPOSED pane, the highlightInput idiom: the module takes
	// plain cells and styles rows whole (doc.go), so a shaded run cannot be handed to it as a cell.
	return m.highlightSettingsEdit(view, display, place)
}

// settingsKeyListSpec composes the key list's [popupSpec] for THIS frame — the display rows, the
// description header and the row budget the frame granted the pane — with the display list it was
// built from. ok is false when the frame cannot seat the pane at all.
//
// It is a step of its own because the painter is not the composition's only reader: the MOUSE maps a
// click back through the very rows, the very header height and the very window that were drawn
// (settingsPaint, mouse.go). Composing once and spending the same numbers twice is what keeps a click
// naming the row under the pointer — a second derivation drifts the first time a description wraps to
// a different number of lines than it assumed.
func (m Model) settingsKeyListSpec(rows []SettingRow) (popupSpec, settingsDisplay, bool) {
	display := m.settingsDisplayRows(rows, m.settingsSelection(len(rows)))
	body := m.settingsBody(rows)
	// The body's own claim is its real height — the fixed description region and the blank under it
	// (settingsBody), or none when no row is highlighted — rather than a taste, exactly as the ask
	// prompt states its question's (popupFloor). Stating it is what seats the header ahead of the
	// list on every window the pane is drawn in: the claim comes off the top of the grant, bounded
	// only by the one line the rows keep for the row the window is anchored on, so it is the LIST
	// that scrolls and never the header that goes.
	maxBody, maxRows, seated := m.popupBudget(paneSettings, len(display.rows), len(display.rows),
		popupChrome, popupFloor{body: popupBodyLineCount(m.th, body, m.width)})
	if !seated {
		return popupSpec{}, settingsDisplay{}, false
	}
	return popupSpec{
		title:       settingsTitle,
		body:        body,
		bodyLead:    settingsDescLabel,
		maxBodyRows: maxBody,
		rows:        display.rows,
		rowKinds:    display.kinds,
		selected:    display.selected,
		hint:        m.settingsPaneHint(rows),
		maxRows:     maxRows,
	}, display, true
}

// renderSettingsText paints the multi-line field a text key's prose is written in — the pane's third
// renderer, and the one that replaces the key LIST rather than one row of it. The frame around it does
// not move: the same border, the same title, and the same description header naming the key being
// written (settingsBody, which reads the SELECTED row and is therefore already about this key), so
// what changes between the list and the field is only what is inside the box.
//
// The field is painted as ROWS because that is what the popup module takes — plain, escape-free cells
// it styles whole (doc.go) — one row per line of the value, every one of them in the edit tone
// (popupRowEditing) so the block reads as the field it is. The caret is a GLYPH inside the line it
// stands on, for the reason it is one on a value row (settingsCaret): a popup row has no seat for the
// terminal's own cursor.
//
// Two things the list does not ask for, both because this is a field and not a list. The rows WRAP
// (popupSpec.wrapRows) — a prompt line longer than the pane must arrive whole, where a key row is
// recognised from its start and can afford the ellipsis — and the selection follows the CARET's line,
// which is what keeps the line being typed inside the scroll window on a prompt longer than the pane
// (popupRowWindow re-derives around it every frame).
func (m Model) renderSettingsText(rows []SettingRow) string {
	spec, ok := m.settingsTextSpec(rows)
	if !ok {
		return "" // the frame cannot seat this pane (settingsGiveWayNote says so on the status line)
	}
	view, place := renderPopupPlaced(m.th, spec, m.width)
	// The drag-selection is overlaid on the COMPOSED field, the key list's own idiom one state along:
	// the module takes plain cells and styles rows whole (doc.go), so a shaded run cannot be handed to
	// it as a cell.
	return m.highlightSettingsText(view, place)
}

// settingsTextLines is the field as the pane PAINTS it: one string per LINE of the value, the caret
// drawn in as a glyph where the next keystroke lands ([lineEditor.textWithCaret]) and every line
// escape-stripped like any other cell handed the popup module (doc.go).
//
// It is a derivation of its own for settingsEditText's reason one state along: the MOUSE reads these
// very strings — a click's column is turned into a rune offset by measuring them, and the highlight
// measures them back the other way (mouse.go) — so the glyph the human clicks on is the glyph the
// caret lands at.
func (m Model) settingsTextLines() []string {
	lines := strings.Split(m.settings.editor.textWithCaret(settingsCaret), "\n")
	for i, line := range lines {
		lines[i] = stripEscapes(line)
	}
	return lines
}

// settingsTextSpec composes the multi-line field's [popupSpec] for THIS frame. It is a step of its own
// for settingsKeyListSpec's reason: the painter is not the composition's only reader, since a click
// maps back through the very rows, the very wrap and the very window that were drawn (settingsTextPaint,
// mouse.go). ok is false when the frame cannot seat the pane at all.
func (m Model) settingsTextSpec(rows []SettingRow) (popupSpec, bool) {
	lines := m.settingsTextLines()
	text := make([]popupRow, 0, len(lines))
	kinds := make([]popupRowKind, 0, len(lines))
	for _, line := range lines {
		text = append(text, popupRow{line})
		kinds = append(kinds, popupRowEditing)
	}
	body := m.settingsBody(rows)
	// The claim is in painted LINES, and a wrapping row costs more than one — so it is measured with
	// the painter's own composition rather than counted off the value, which would ask for too few the
	// moment a line wrapped and hide the tail of a prompt the pane had room for.
	claim := popupRowBlockLines(popupRowHeights(popupRowBlocks(m.th, text, true, popupInnerWidth(m.th, m.width))), 0, 0)
	maxBody, maxRows, seated := m.popupBudget(paneSettings, claim, claim, popupChrome,
		popupFloor{body: popupBodyLineCount(m.th, body, m.width)})
	if !seated {
		return popupSpec{}, false
	}
	return popupSpec{
		title:       settingsTitle,
		body:        body,
		bodyLead:    settingsDescLabel,
		maxBodyRows: maxBody,
		rows:        text,
		rowKinds:    kinds,
		wrapRows:    true,
		selected:    clampInt(m.settings.editor.caretLine(), 0, len(text)-1),
		hint:        m.settingsPaneHint(rows),
		maxRows:     maxRows,
	}, true
}

// renderSettingsEnum paints the value sub-list — the second step of an enum edit — in the SAME pane,
// as a MENU rather than as a list (popupSpec.menuRows): four values a human is choosing between, all
// of them on the screen at once, which is the approval prompt's shape and not the picker's scrolled
// offering. The pane it replaces is still the one the frame allocated, so nothing about the frame
// moves between the two steps except what is inside the border.
//
// It is SHORTER than the key list it replaced — a vocabulary is a handful of rows — and the rows it
// does not use go back to the transcript for as long as the question is open. That is the row plan
// working as written (frameRowPlan grants the pane the budget; the pane takes what it needs): the
// full-height rule is what /settings claims when it lists every key, not a height it holds empty.
//
// The body names the key being set and what it is for, because the list behind it — where the human
// read the key's name — is the thing this pane just replaced. The value the key already holds carries a
// "(current)" cell of its own, so the question can be answered without remembering the answer to it.
func (m Model) renderSettingsEnum(row SettingRow) string {
	current := m.settingsCurrentValue(row)
	vocabulary := m.settingsVocabulary(row)
	values := make([]popupRow, 0, len(vocabulary))
	for _, value := range vocabulary {
		cell := ""
		if value == current {
			cell = "(current)"
		}
		values = append(values, popupRow{stripEscapes(value), cell})
	}
	body := truncateToWidth(m.th, stripEscapes(settingsEnumPrompt(row)), popupInnerWidth(m.th, m.width))
	maxBody, maxRows, seated := m.popupBudget(paneSettings, len(values), len(values),
		popupChrome, popupFloor{body: popupBodyLineCount(m.th, body, m.width)})
	if !seated {
		return "" // the frame cannot seat this pane (settingsGiveWayNote says so on the status line)
	}
	return renderPopup(m.th, popupSpec{
		title:       settingsTitle,
		body:        body,
		maxBodyRows: maxBody,
		rows:        values,
		menuRows:    true,
		selected:    clampInt(m.settings.sub, 0, len(values)-1),
		hint:        settingsEnumHint,
		maxRows:     maxRows,
	}, m.width)
}

// settingsEnumPrompt is the sub-list's one-line question: the key, then what it is for. Two facts on
// one line because that is the whole budget a pane body is kept (settingsBody) — and the key's name
// first, because it is the one the human needs in order to know what they are answering.
func settingsEnumPrompt(row SettingRow) string {
	if row.Desc == "" {
		return row.Path
	}
	return row.Path + " — " + row.Desc
}

// settingsGiveWayNote is what the status line says for a settings pane the frame could not seat —
// under twelve rows, or under a window a long draft has taken the last of. It is the licence
// layout.md gives every surface that disappears (the band's "N queued" is the other), and here it
// carries the ONE fact the human cannot do without: the pane is open, it is swallowing every key,
// and esc is the way out. Without it a short terminal would answer /settings with a frame that
// looked idle and a keyboard that did nothing.
const settingsGiveWayNote = "settings — esc close"

// settingsGiveWayPhrase is that note when it is owed and "" otherwise — the string statusLeft puts
// in the slot an idle frame otherwise leaves empty.
func (m Model) settingsGiveWayPhrase() string {
	if !m.settings.open || m.settingsSeated() {
		return ""
	}
	return m.th.statusBar.Render(settingsGiveWayNote)
}

// settingsSeated reports whether the frame's allocation leaves room to draw the pane at all. It asks
// popupBudget — the same seam renderSettings asks — with no rows, because "seated" turns on the
// pane's CHROME against its grant and never on how many rows it holds: one derivation, two callers,
// so the status line can never claim a give-way the pane did not take (or stay silent about one it
// did).
func (m Model) settingsSeated() bool {
	_, _, seated := m.popupBudget(paneSettings, 0, 0, popupChrome, popupFloor{})
	return seated
}
