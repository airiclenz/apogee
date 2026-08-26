package tui

import (
	"image/color"
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
// seam ([SettingsHost.Rows]) — the renderer holds no schema, reads no file and knows no
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
// opens the value sub-list, and what is committed goes out through [SettingsHost.Write] — the
// binary's comment-preserving splice writer, never this package's idea of YAML. The renderer's whole
// half of an edit is the ORDER (which key, which value) and what the row says afterwards. Nothing is
// re-read to find out: the pane records what it persisted ([Model.settingEdits]) and marks the row,
// because every other row is still showing the value THIS run resolved and a mid-session file read
// would leave one row disagreeing with its neighbours about which run it is describing.
//
// And what is persisted is APPLIED, on the same ⏎ (ADR 0037 decision 1): the pane routes the key to
// whatever puts it into effect — a field of its own for the keys whose effect is this screen, and
// [SettingsHost.Apply] for every key the engine or the composition root owns — so the session runs
// what the file says the moment the file says it. A key that can only land at a boundary the session
// will cross anyway says so on the row ("· applies at next clear"); a key that could only land at the
// next start does not exist. What a row keeps afterwards is a ` *` (settingsEditMarker), which says
// this session changed the key here rather than that anything is still pending.
//
// A string or an int is edited in a BUFFER on its own row (the /sessions rename idiom): ⏎ opens it,
// the row's value cell becomes what is being typed with a caret after it, ⏎ commits and esc
// abandons. What a commit is checked against is the BINARY's business, exactly as the file format is
// — [SettingsHost.Write] refuses a value the key cannot hold (a port outside its range, an
// endpoint with no host) and the refusal lands on the row with the buffer still open, so the human
// corrects what they typed rather than typing it again.
//
// And backspace UNSETS: it arms a reset on a row that has something to reset, the hint line asks for
// a confirming ⏎, and what that ⏎ sends is [SettingsHost.Reset] — the key's line REMOVED from the
// file rather than today's spelling of its default written into it (ADR 0035). The row then reports
// the default it went back to, on exactly the terms a write reports its value — and the default is
// APPLIED exactly as a written value is, so a reset cannot mean less to the session than a write.

// settingsKind is what the open pane is DOING: reading its key list, asking which value one enum key
// should take, switching individual Mechanisms in the `mechanisms` row's own list, holding the buffer
// a string or an int is being typed into, waiting for a reset to be confirmed, or holding the
// multi-line field a text key's prose is written in. It is the picker's own
// two-step idiom (pickerKind, /schedule's cycle-then-mode pair): one pane, one selection per step, and
// the step is a field rather than a second overlay — so there is no state in which two settings
// surfaces are open and no second give-way rule to write.
//
// One field for all six is also what makes them mutually exclusive by construction: a pane cannot be
// buffering a value and awaiting a reset confirmation at once, so no keypress has two meanings and no
// state pair has to be reasoned about.
type settingsKind int

const (
	settingsKeyList       settingsKind = iota // the key list — the pane's own screen
	settingsEnumList                          // the selected enum key's closed vocabulary, one value per row
	settingsMechanismList                     // the `mechanisms` row's catalogue, one Mechanism per row, switched in place
	settingsValueBuffer                       // the selected string/int key's edit buffer, on its own row
	settingsResetArmed                        // backspace armed the selected row's reset; ⏎ confirms it
	settingsTextEditor                        // the selected text key's prose, in a multi-line field filling the pane
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
// level up, on the Model ([Model.settingEdits]). sub is a sub-list's highlight, meaningful only while
// kind is [settingsEnumList] or [settingsMechanismList] — the two steps that replace the key list with
// a list of their own; editor is the field a string or an int is typed into, meaningful only while
// kind is [settingsValueBuffer]. Both selections are [listCursor]s: the clamp, the wrap rule and the
// verdict each key earns are the package's one answer to those questions (listsurface.go, ADR 0053),
// and what stays here is what only this pane knows — which row a ⏎ opens, and what backspace arms.
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
	open bool
	kind settingsKind
	// listCursor is the KEY list's highlight and the key contract that walks it (listsurface.go,
	// ADR 0053), embedded rather than re-declared so `m.settings.selected` still reads as the pane's
	// own while there is one answer to what ↑/↓, ⏎ and esc do inside a modal list.
	listCursor
	// sub is a SUB-LIST's highlight, meaningful only while kind is [settingsEnumList] or
	// [settingsMechanismList]. It is a second cursor rather than a second surface, and NAMED rather
	// than embedded, because a pane cannot embed two of anything and because neither sub-list filters:
	// a cursor is eight bytes where a filtering surface would have brought a whole text widget with it
	// (ADR 0053 decision 9). The pane's one field ([lineEditor]) stays the one it already had.
	sub     listCursor
	editor  lineEditor
	sel     promptSel
	failure settingFailure
	answer  settingAnswer
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
// The Mechanism list's legend names a SECOND key on the same act and no commit at all, because that
// list is switches rather than a question: ⏎ and space both flip the highlighted row, each flip is
// its own persisted edit (ADR 0035), and esc only ends a list nothing is pending in — where the enum
// sub-list's ⏎ is the one press that answers it.
const (
	settingsTitle         = "Settings"
	settingsHint          = "↑/↓ select · ⏎ edit · ⌫ reset · esc close"
	settingsNoResetHint   = "↑/↓ select · ⏎ edit · esc close"
	settingsEnumHint      = "↑/↓ select · ⏎ set · esc back"
	settingsMechanismHint = "⏎/space toggle · esc back"
	settingsBufferHint    = "⏎ save · esc cancel"
	settingsResetHint     = "⏎ confirm reset · esc cancel"
	settingsTextHint      = "ctrl+s save · esc discard"
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
//
// On the `mode` row the value beside it is always the LIVE rung — Shift+Tab moves the session
// without ever writing a journal entry — and the marker only says the session wrote the key once.
const settingsEditMarker = " *"

// The value cells of a bool row, spelled as the config file spells them — the two strings ⏎ toggles
// between and hands [SettingsHost.Write], which is the whole of what "the value as the file would
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

// settingRows is the pane's rows as they stand RIGHT NOW: what [SettingsHost.Rows] answers, or
// nothing at all when no host is wired. Every reader goes through here — the open degrade, the
// key routing's clamp, the renderer — so the count the selection is clamped against and the list the
// pane paints are the same derivation asked twice rather than two guesses at it.
func (m Model) settingRows() []SettingRow {
	if m.opts.Settings == nil {
		return nil
	}
	return m.opts.Settings.Rows()
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
		m.settings.kind, m.settings.sub = settingsKeyList, listCursor{}
		m.layout()
		return m, nil
	}
	if m.settings.kind == settingsMechanismList {
		if _, toggles, ok := m.settingsMechanismTarget(rows); ok {
			return m.settingsMechanismKey(msg, toggles)
		}
		// The catalogue went away under the list — an unwired seam, or a row that is no longer the
		// `mechanisms` row. Same fallback and the same swallow as the sub-list above: a ⏎ aimed at a
		// switch must not land on whatever the key list now highlights.
		m.settings.kind, m.settings.sub = settingsKeyList, listCursor{}
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
	switch m.settings.key(msg, n, listWrapsAround) {
	case listCloses:
		// The overlay goes whole — highlight, step, buffer, last refusal — and the session's edit
		// journal does NOT: it is on the Model, and the ` *` markers it carries describe a session that
		// is still running the values it recorded (ADR 0037 decision 8).
		m.settings = settingsPane{}
		m.layout()
		return m, nil
	case listAccepts:
		return m.settingsEnter(rows)
	case listSwallowed:
		return m, nil // an arrow the cursor spent, or a ⏎ over a list with no rows to open
	case listUnclaimed:
	}
	// backspace is the one key of this pane that no list has — it UNSETS rather than edits — so it is
	// answered here, out of what the shared contract found no use for. Everything else is swallowed:
	// this is a full-height modal, and a key falling through it would land in a chat box the human
	// cannot see (handleKey's own reason one surface down).
	if msg.String() == "backspace" {
		return m.settingsArmReset(rows)
	}
	return m, nil
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
	m.settings.kind, m.settings.sub, m.settings.editor = settingsKeyList, listCursor{}, lineEditor{}
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
	if row.Path == settingKeyMechanisms {
		// The one structured row this pane DOES open: its children are switches, and a list of switches
		// is a shape a row list holds perfectly well. Matched on its path for settingsVocabulary's
		// reason one row over — what makes it special is where its vocabulary comes from (the Mechanism
		// catalogue, [Options.ListMechanisms]) and not a kind of its own. An unwired seam offers
		// nothing, so ⏎ opens nothing, exactly as an enum with no vocabulary does below.
		if len(m.settingsMechanisms(rows)) == 0 {
			return m, nil
		}
		m.settings.kind, m.settings.sub = settingsMechanismList, listCursor{}
		m.layout()
		return m, nil
	}
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
		// presses ⏎ twice has then confirmed what was already set — which config.SaveConfigSetting writes
		// nothing for — where a highlight reset to the first row would have silently changed the key.
		m.settings.kind = settingsEnumList
		m.settings.sub.selected = max(0, indexOfSetting(values, m.settingsCurrentValue(row)))
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
	switch m.settings.sub.key(msg, len(values), listWrapsAround) {
	case listCloses:
		m.settings.kind, m.settings.sub = settingsKeyList, listCursor{}
		m.layout()
		return m, nil
	case listAccepts:
		// The cursor clamped itself against this composition of the vocabulary before it answered, so
		// the highlighted value is a value this list has.
		value := values[m.settings.sub.selected]
		m.settings.kind, m.settings.sub = settingsKeyList, listCursor{}
		if row.Kind == SettingServer {
			return m.settingsSwitchServer(row, value)
		}
		return m.settingsWrite(row, value)
	case listSwallowed, listUnclaimed:
	}
	return m, nil // swallowed, like every key the pane does not act on
}

// settingsMechanismKey routes a keypress in the Mechanism list: ↑/↓ walk the catalogue with the wrap
// the key list and the value sub-list both use, ⏎ and space each flip the highlighted Mechanism, and
// esc returns to the key list. Every other key is swallowed, the pane's modality wherever it stands.
//
// The list STAYS OPEN across a flip, which is what makes it a switch panel rather than a question:
// setting a posture is usually several switches, and a list that closed on each would have to be
// re-opened and re-walked between them. That changes what the human presses NEXT and not what a press
// means — each flip is still one deliberate edit, persisted on its own (ADR 0035).
//
// Two keys for the one act because both are already true of it: ⏎ is what every other row of this
// pane acts on, and space is what a list of switches is ticked with (the ask prompt's multi-select).
func (m Model) settingsMechanismKey(msg tea.KeyPressMsg, toggles []MechanismToggle) (tea.Model, tea.Cmd) {
	n := len(toggles)
	switch m.settings.sub.key(msg, n, listWrapsAround) {
	case listCloses:
		m.settings.kind, m.settings.sub = settingsKeyList, listCursor{}
		m.layout()
		return m, nil
	case listAccepts:
		return m.settingsToggleMechanism(toggles[m.settings.sub.selected])
	case listSwallowed:
		return m, nil
	case listUnclaimed:
	}
	// space is the second key on the one act and no list's, so it is answered here, out of what the
	// shared contract handed back. Why the act has two keys is above.
	if msg.String() == "space" && n > 0 {
		return m.settingsToggleMechanism(toggles[m.settings.sub.selected])
	}
	return m, nil // swallowed, like every key the pane does not act on
}

// settingsToggleMechanism flips one Mechanism through [Options.WriteMechanism] — persisted AND put in
// force behind that single seam, rather than through the pane's own write-then-apply pair: a
// Mechanism id is not a registry key, so there is no path for [SettingsHost.Apply] to be handed and
// no second door to keep in step with this one.
//
// The outcomes are settingsPersist's, minus the one this act does not have — and the seam's `saved`
// half is what keeps the two failing ones apart, exactly as settingsApplied keeps them apart for
// every registry key. No seam wired, or a REFUSED splice (!saved), leaves the block exactly as it was
// and says so on the `mechanisms` row the list belongs to — where the human reads it when the list
// closes, since the list itself is switches and has no note column. A splice that landed under a
// failed apply says the same sentence prefixed by settingsApplyFailedNote, because the file now
// carries the flip the session is not running and "unchanged" would be a lie about it. A flip that
// LANDED WHOLE records nothing at all: the list is re-read from the file on the very next frame
// ([Model.settingsMechanisms]), so what it paints is what the file now carries rather than what this
// keypress hoped for, and that is a truer marker than a journal entry could be.
func (m Model) settingsToggleMechanism(toggle MechanismToggle) (tea.Model, tea.Cmd) {
	if m.opts.WriteMechanism == nil {
		m.settings.failure = settingFailure{path: settingKeyMechanisms, msg: noSettingsWriterNote}
		m.layout()
		return m, nil
	}
	if saved, err := m.opts.WriteMechanism(toggle.ID, !toggle.Enabled); err != nil {
		note := err.Error()
		if saved {
			note = settingsApplyFailedNote + note
		}
		m.settings.failure = settingFailure{path: settingKeyMechanisms, msg: note}
		m.layout()
		return m, nil
	}
	// The refusal a previous flip left is gone with the flip that landed, exactly as recordSettingEdit
	// clears it for every other row: the slot describes the LAST attempt, and this attempt worked.
	m.settings.failure = settingFailure{}
	m.layout()
	return m, nil
}

// settingsSwitchServer answers the `server` row's popup, and what it does is the whole of `/server`
// (ADR 0037 decision 4): the session MOVES to the chosen entry, and the move records that entry as
// the one the next session starts on — which is this key's entire persistence, so no [SettingsHost.Write]
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
	if choice.Name == m.opts.HostAlias {
		// The delegate's answer is a transcript note, and this pane is drawn over the transcript — so
		// from in here the ⏎ that confirmed the current server would look like a keypress that did
		// nothing. The same sentence lands on the row (settingAnswer), where the human who pressed it is
		// looking. It is not a failure and not an edit: nothing was refused and nothing changed, which is
		// why it clears the failure slot rather than filling it.
		m.settings.answer = settingAnswer{path: row.Path, msg: settingsAlreadyOnNote + stripEscapes(choice.Name)}
		m.settings.failure = settingFailure{}
		return m.switchToServer(choice)
	}
	if !m.serverActs().CanSwitch {
		return m.settingsFailed(row, noServerSwitchNote)
	}
	from := hostDisplay(m.opts) // the label the footer used for the old server, captured before it moves
	result, err := m.opts.Server.Switch(choice.Name)
	if err != nil {
		return m.settingsFailed(row, stripEscapes(err.Error()))
	}
	m = m.recordSettingEdit(settingEdit{path: row.Path, value: choice.Name})
	return m.foldServerSwitch(from, result, recordServerChoice(m.opts.Server, choice.Name))
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

// newSettingsEditor is the field a string or an int row is typed into: this pane's naming of the
// popup-painted field every overlay here types into ([newPopupField], lineeditor.go — the chat box's
// own field, minus the chat), seeded with what the key holds and with the caret at the end of it,
// which is where a human correcting a port starts.
//
// Single-line is the whole configuration it needs: ⏎ then belongs to this pane (commit) rather than to
// the widget, and a scalar config value has no second line to walk to. shape is the `cursor-shape`
// key's selection, passed for the same reason the prompt takes it — the field is built the same way
// wherever it is built. settingsCaret is this pane's own caret glyph, and the field carries it from
// here on rather than being handed it again where the row is painted (settingsEditText).
func newSettingsEditor(shape tea.CursorShape, surface color.Color, seed string) lineEditor {
	return newPopupField(shape, surface, settingsCaret, seed)
}

// settingsEditKey routes the keys the pane's two typed states — the one-line value buffer
// (settingsBufferKey) and the multi-line prose field (settingsTextKey) — answer the SAME way: esc
// abandons the edit, and every other key goes to the FIELD (lineEditor.editKey), which is the whole of
// the editing this pane does not write itself. What the human gets there is what the chat box gives:
// insertion at the caret, backspace and delete around it, ←/→ and the word jumps, home/end (spec
// requirement 7).
//
// The COMMIT key is deliberately NOT here. It is the one key the two states disagree on — ⏎ for a
// scalar that has no second line to walk to, ctrl+s for a field where ⏎ means what it means in any
// editor (ADR 0037 decision 10) — so each state claims its own before reaching this, and the
// difference stays readable at the two call sites rather than as a parameter naming a keystroke.
//
// relayout is the second thing they disagree on, and it IS a parameter because it is not a key: the
// multi-line field is the pane's row list, so a line added or removed changes how many rows the pane
// measures, where the one-line buffer is one cell of one row and changes nothing it measures.
//
// Backspace reaches the field in both states rather than arming a reset: inside an edit it means what
// it means in every other text field on the screen. That is exactly why the two idioms can share the
// key — the pane's kind says which of them is being typed at, and the field claims backspace only
// while it is open.
func (m Model) settingsEditKey(msg tea.KeyPressMsg, relayout bool) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		// An abandoned edit takes its refusal with it: the ✗ on this row is the reason THIS edit was
		// not accepted, and leaving it up after the human walked away from the edit would report a
		// failure against a row nobody is editing any more.
		m.settings.kind, m.settings.editor = settingsKeyList, lineEditor{}
		m.settings.failure = settingFailure{}
		m.layout()
		return m, nil
	}
	// Whatever Cmd the widget asks for is returned rather than dropped, exactly as the chat box
	// returns it (model.go) — a single-line field asks for none today (lineEditor.singleLine), and
	// swallowing one silently is how that stops being true unnoticed.
	cmd := m.settings.editor.editKey(msg)
	if relayout {
		m.layout()
	}
	return m, cmd
}

// settingsCommitEdit persists what a typed state committed, and stays in the edit when the binary will
// not have it. That is the whole reason a commit reports its outcome: a refused value is still on the
// screen, with the reason beside it (settingsNote), so the human fixes a port they mistyped instead of
// typing it again from nothing.
//
// The value is checked by the BINARY, not here (ADR 0011's thin renderer): what a key may hold is the
// registry's business and it is the write seam that asks — this pane knows only that a refusal means
// the file is unchanged.
//
// The trim is the CALLER's and so is blank, its verdict that there is nothing to commit: the two
// states trim differently and therefore disagree about what "nothing" is (settingsCommitBuffer,
// settingsCommitText), and both verdicts are preserved exactly as they were written. A blank edit
// commits nothing at all and simply closes, the /sessions empty-rename posture: a field the human has
// just cleared is far more likely to be an abandoned edit than a request to persist emptiness, and the
// deliberate way to take a value away is the reset backspace arms.
func (m Model) settingsCommitEdit(row SettingRow, value string, blank bool) (tea.Model, tea.Cmd) {
	if blank {
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

// settingsBufferKey routes a keypress in the string/int edit buffer: ⏎ commits, and every other key is
// the shared edit contract's (settingsEditKey — esc abandons, the rest goes to the field). ⏎ is this
// state's commit because a scalar config value has no second line to walk to, so the widget's newline
// binding is free for it (newSettingsEditor).
//
// The frame is NOT laid out again on an edited character: the buffer is one cell of one row, so
// nothing typed into it changes what the pane measures.
func (m Model) settingsBufferKey(msg tea.KeyPressMsg, row SettingRow) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		return m.settingsCommitBuffer(row)
	}
	return m.settingsEditKey(msg, false)
}

// settingsCommitBuffer commits the string/int buffer — the shared commit (settingsCommitEdit) against
// this state's own trim. TrimSpace, because a scalar value is a token (a port, a path, a model id) and
// the whitespace around one is never part of it: it is also what lets a path pasted with its line
// ending still on it commit as the path, since the widget folds that ending to a space
// ([lineEditor.flattenLine]). The buffer is therefore blank exactly when that trim leaves nothing.
func (m Model) settingsCommitBuffer(row SettingRow) (tea.Model, tea.Cmd) {
	value := stripEscapes(strings.TrimSpace(m.settings.editor.value()))
	return m.settingsCommitEdit(row, value, value == "")
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
// the widget's idea of visual rows would step through wraps the pane never drew. Multi-line is why it
// is not a [newPopupField]; the caret glyph it carries is the buffer field's own (settingsCaret),
// because both are painted into the same pane's rows.
func newSettingsTextEditor(shape tea.CursorShape, surface color.Color, seed string) lineEditor {
	e := newLineEditor(shape, surface, settingsCaret)
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

// settingsTextKey routes a keypress in the multi-line field: ctrl+s commits, and every other key is
// the shared edit contract's (settingsEditKey — esc abandons, the rest goes to the field, ⏎ among
// them, which is the whole difference between this step and the one-line buffer).
//
// esc discarding rather than committing is deliberate and is what the legend says (settingsTextHint):
// the field holds a page of prose, and the key that walks away from an edit must not be the one that
// persists it. The frame IS laid out again on an edited character, unlike the buffer's: this field is
// the pane's row list, so a line added or removed changes how many rows the pane measures.
func (m Model) settingsTextKey(msg tea.KeyPressMsg, row SettingRow) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+s" {
		return m.settingsCommitText(row)
	}
	return m.settingsEditKey(msg, true)
}

// settingsCommitText commits the prose field — the shared commit (settingsCommitEdit) against this
// state's own trim. TrimRight of newlines and nothing else, because the spaces inside a prompt and the
// indentation of its lines ARE the prompt; what goes is the blank last line a trailing ⏎ leaves behind
// (the writer normalizes a block scalar to exactly one, settingsTextValue).
//
// "Nothing to commit" is therefore whitespace-only prose rather than the empty string, which is where
// this state parts from the buffer's verdict: a field cleared back to spaces is as abandoned as one
// cleared to nothing, and the deliberate way to take the prompt away is the reset backspace arms
// (which the binary's own validator says in as many words).
func (m Model) settingsCommitText(row SettingRow) (tea.Model, tea.Cmd) {
	value := strings.TrimRight(stripEscapes(m.settings.editor.value()), "\n")
	return m.settingsCommitEdit(row, value, strings.TrimSpace(value) == "")
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

// settingsMechanismTarget is the row an open Mechanism list belongs to and the catalogue it is
// showing, and whether there still IS one — settingsEnumTarget's contract for the pane's third
// list-shaped step, and the single predicate the key router, the renderer and the pointer all branch
// on so none of them can think a different list is up.
func (m Model) settingsMechanismTarget(rows []SettingRow) (SettingRow, []MechanismToggle, bool) {
	if m.settings.kind != settingsMechanismList {
		return SettingRow{}, nil, false
	}
	row, ok := m.settingsSelectedRow(rows)
	if !ok {
		return SettingRow{}, nil, false
	}
	toggles := m.settingsMechanisms(rows)
	if len(toggles) == 0 {
		return SettingRow{}, nil, false
	}
	return row, toggles, true
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
// built-ins plus whatever `*.yaml` files the human's schemes folder holds right now ([SchemeHost.List]),
// which no static table can name either. It reaches the pane as an ordinary enum (cmd/apogee's
// settingKind) because picking one is picking a value from a list — only where the list COMES from
// differs — so it is matched on its path rather than on a kind of its own.
//
// Every step of the sub-list asks this — the open, the walk, the accept, the paint — so a list that
// changed under an open question is one list wherever it is read, and the accept can only ever take a
// value the frame the human answered was showing.
func (m Model) settingsVocabulary(row SettingRow) []string {
	if row.Path == settingKeyColorScheme {
		if m.opts.Schemes == nil {
			return nil // unwired: the row opens nothing rather than offering an empty list
		}
		return m.opts.Schemes.List()
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

// settingsMechanisms is the catalogue the `mechanisms` row's list offers — every Mechanism this build
// knows, with the file's own on/off for each ([Options.ListMechanisms]) — and nothing at all wherever
// there is no such list to draw: an unwired seam, or a selection that is not that row.
//
// It is settingsVocabulary's counterpart for the one row whose vocabulary is neither a registry
// vocabulary nor a config block, and it is asked at every step for the same reason — the open, the
// walk, the flip, the paint — so a catalogue re-read under an open list is one list wherever it is
// read, and a flip can only ever act on a Mechanism the frame the human pressed was showing.
//
// Re-reading it per ask is also what makes an edit made in the FILE show here: the seam loads the
// block fresh, so a `mechanisms:` line changed in another window is on the next frame of an open list
// rather than at the next start.
func (m Model) settingsMechanisms(rows []SettingRow) []MechanismToggle {
	if m.opts.ListMechanisms == nil {
		return nil
	}
	row, ok := m.settingsSelectedRow(rows)
	if !ok || row.Path != settingKeyMechanisms {
		return nil
	}
	return m.opts.ListMechanisms()
}

// settingsCurrentValue is the value a sub-list opens on and marks "(current)": what the pane believes
// the file holds (settingsPersistedValue) for every key but two.
//
// The `server` row is the first, and its honest answer is the entry the session is ON — identified by
// name, the picker's own comparison ([Model.currentServerRow]) — rather than the entry the key names:
// `/server` moves a session and rewrites the key without this pane ever hearing about it, so a value
// read off the launch resolution would mark the server the session has left. Name and not endpoint,
// so that a sibling entry sharing the bound one's URL is not marked as the one you are on.
//
// The `mode` row is the second, and for the same reason with a different mover: Shift+Tab cycles the
// rung without writing anything at all, so the journal and the file both go on describing a session
// that has moved. Its answer is the row's own value, which the host overlays from the live engine —
// so the sub-list opens on the rung the session is RUNNING, and ⏎ on the row marked "(current)"
// re-applies that rung rather than a stale one.
func (m Model) settingsCurrentValue(row SettingRow) string {
	if row.Kind == SettingServer {
		for _, choice := range m.servers() {
			if choice.Name == m.opts.HostAlias {
				return choice.Name
			}
		}
	}
	if row.Path == settingKeyMode {
		return row.Value
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

// settingsSelection is which SETTING row is highlighted, clamped to a list of n rows: −1 when there
// are none, which is renderPopup's own "no highlight" convention. It is this pane's naming of the one
// clamp every list in the package answers through ([listCursor.highlight], listsurface.go), and the
// one place the stored index is turned into an index anything may be read by — so a stale selection
// cannot reach a slice, and it cannot be clamped one way here and another way in a sibling pane.
func (m Model) settingsSelection(n int) int {
	return m.settings.highlight(n)
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
	return stripEscapes(m.settings.editor.textWithCaret())
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
//
// `mode` is the row where that reasoning inverts: the rung moves from OUTSIDE this pane too
// (Shift+Tab), so the journal is not the last word on it and the provider is — the host overlays the
// row from the live engine. The cell therefore shows the row's value whatever the journal holds, and
// the journal decides only whether the marker is on it.
func (m Model) settingsValueCell(row SettingRow) string {
	edit, ok := m.settingEditOf(row.Path)
	if !ok {
		return row.Value
	}
	if row.Path == settingKeyMode {
		return row.Value + settingsEditMarker
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
	if row, toggles, ok := m.settingsMechanismTarget(rows); ok {
		return m.renderSettingsMechanisms(row, toggles)
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
		scrollbar:   m.popupScrollbarOn(),
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
	lines := strings.Split(m.settings.editor.textWithCaret(), "\n")
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
		scrollbar:   m.popupScrollbarOn(),
	}, true
}

// renderSettingsSubList paints a second-step sub-list in the pane the key list was read in — the enum
// vocabulary (renderSettingsEnum) and the Mechanism catalogue (renderSettingsMechanisms), which are one
// surface with two contents. Everything that makes it that surface is stated once here: the pane it
// claims, the title over it, the body naming the key being answered (settingsEnumPrompt, because the
// list where the human read that name is what this replaced), the MENU shape (listContent.menuRows —
// choices a human takes in at once, not a scrolled offering), the window left to the row plan, and the
// highlight the sub-list's shared cursor clamps.
//
// What each content brings is its ROWS and its LEGEND — a vocabulary with one "(current)" cell against
// a catalogue of switches that each carry their own — so those are the only two parameters.
func (m Model) renderSettingsSubList(row SettingRow, values []popupRow, hint string) string {
	return m.renderList(listContent{
		pane:     paneSettings,
		title:    settingsTitle,
		body:     truncateToWidth(m.th, stripEscapes(settingsEnumPrompt(row)), popupInnerWidth(m.th, m.width)),
		hint:     hint,
		rowCap:   len(values), // the whole content is the taste; popupBudget answers with the frame's
		rows:     values,
		menuRows: true,
		selected: m.settings.sub.highlight(len(values)),
	})
}

// renderSettingsEnum paints the value sub-list — the second step of an enum edit — in the SAME pane,
// as a MENU rather than as a list (renderSettingsSubList): four values a human is choosing between,
// all of them on the screen at once, which is the approval prompt's shape and not the picker's
// scrolled offering. The pane it replaces is still the one the frame allocated, so nothing about the
// frame moves between the two steps except what is inside the border.
//
// It is SHORTER than the key list it replaced — a vocabulary is a handful of rows — and the rows it
// does not use go back to the transcript for as long as the question is open. That is the row plan
// working as written (frameRowPlan grants the pane the budget; the pane takes what it needs): the
// full-height rule is what /settings claims when it lists every key, not a height it holds empty.
//
// The body names the key being set and what it is for, because the list behind it — where the human
// read the key's name — is the thing this pane just replaced. The value the key already holds carries a
// "(current)" cell of its own, so the question can be answered without remembering the answer to it —
// and that cell is the whole of what this content brings to the shared painter.
func (m Model) renderSettingsEnum(row SettingRow) string {
	current := m.settingsCurrentValue(row)
	vocabulary := m.settingsVocabulary(row)
	values := make([]popupRow, 0, len(vocabulary))
	for _, value := range vocabulary {
		cell := m.settingsEnumValueCell(row, value)
		switch {
		case value == current && cell != "":
			cell += " (current)"
		case value == current:
			cell = "(current)"
		}
		values = append(values, popupRow{stripEscapes(value), cell})
	}
	return m.renderSettingsSubList(row, values, settingsEnumHint)
}

// settingsEnumValueCell is what the sub-list's right-hand column says about ONE value BEFORE the
// "(current)" marker joins it — nothing for a plain vocabulary, where the marker is the whole of the
// column.
//
// The `mode` row's `auto` is the exception: it carries the blast radius of the rung it offers
// (autoBlastRadiusLine, the same sentence /confine paints), so what an escalation costs is on the
// screen while the question is still open and not only in the note that follows the ⏎.
func (m Model) settingsEnumValueCell(row SettingRow, value string) string {
	if row.Path == settingKeyMode && domain.Mode(value) == domain.ModeAuto {
		return autoBlastRadiusLine(m.opts.Confinement, m.eng.ConfineToWorkspace())
	}
	return ""
}

// settingsMechanismState is what a Mechanism's row says about itself: the whole cell, because a
// switch has exactly two positions and a blank one would read as a row that failed to render.
const (
	settingsMechanismOn  = "on"
	settingsMechanismOff = "off"
)

// renderSettingsMechanisms paints the `mechanisms` row's own list — the pane's fourth renderer, and
// the second content the shared sub-list draws (renderSettingsSubList: a MENU in the same pane, the
// same frame around it, the body naming the key because the list where the human read that name is
// what this replaced). What this function IS, is the one difference the content forces: the rows are
// SWITCHES, so every one carries its own state cell rather than one of them carrying "(current)", and
// the legend says what flips them (settingsMechanismHint).
//
// It is also the one list of this pane that reliably overflows — the catalogue is twenty-one
// Mechanisms and counting — which is why the shared painter leaves the window to the row plan exactly
// as the key list does (popupBudget answers with the frame's grant) and the bar the overflow earns
// comes from the same place every other popup's does (popupSpec.scrollbar).
//
// Nothing here says what a Mechanism DOES: the id is what the config file names it by and what the
// documentation indexes it under, and a sentence per row would make a manual of a switch panel
// (ADR 0035's one-deliberate-edit surface is not a place to learn what to edit).
func (m Model) renderSettingsMechanisms(row SettingRow, toggles []MechanismToggle) string {
	values := make([]popupRow, 0, len(toggles))
	for _, toggle := range toggles {
		state := settingsMechanismOff
		if toggle.Enabled {
			state = settingsMechanismOn
		}
		values = append(values, popupRow{stripEscapes(toggle.ID), state})
	}
	return m.renderSettingsSubList(row, values, settingsMechanismHint)
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
