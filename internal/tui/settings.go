package tui

import (
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
// past its end. What the provider answers is the resolution THIS run made — which is why an edit
// persisted below is reported as a MARKER on the row rather than as a new value: the pane holds no
// copy of the config that could disagree with the file.
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
// marker, the mask, the "edit in config.yaml" pointer — is already on the row (see [SettingRow]), so
// nothing here formats a config value or decides what may be written.
//
// And it WRITES, one key per deliberate edit (ADR 0035): ⏎ on a bool row toggles it, ⏎ on an enum row
// opens the value sub-list, and what is committed goes out through [Options.WriteSetting] — the
// binary's comment-preserving splice writer, never this package's idea of YAML. The renderer's whole
// half of an edit is the ORDER (which key, which value) and what the row says afterwards. Nothing is
// re-read to find out: the pane records what it persisted ([settingsPane.edits]) and marks the row
// "(next launch)", because every other row is still showing the value THIS run resolved and a
// mid-session file read would leave one row disagreeing with its neighbours about which run it is
// describing. The one exception is `mode`, which has a live seam (Engine.SetMode) — the same one
// Shift+Tab drives — so its edit takes effect now and the row simply shows the new value.
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
// the default it went back to, on exactly the terms a write reports its value: a marker, or — for
// `mode` — the live apply and no marker at all.

// settingsKind is what the open pane is DOING: reading its key list, asking which value one enum key
// should take, holding the buffer a string or an int is being typed into, or waiting for a reset to
// be confirmed. It is the picker's own two-step idiom (pickerKind, /schedule's cycle-then-mode pair):
// one pane, one selection per step, and the step is a field rather than a second overlay — so there
// is no state in which two settings surfaces are open and no second give-way rule to write.
//
// One field for all four is also what makes them mutually exclusive by construction: a pane cannot be
// buffering a value and awaiting a reset confirmation at once, so no keypress has two meanings and no
// state pair has to be reasoned about.
type settingsKind int

const (
	settingsKeyList     settingsKind = iota // the key list — the pane's own screen
	settingsEnumList                        // the selected enum key's closed vocabulary, one value per row
	settingsValueBuffer                     // the selected string/int key's edit buffer, on its own row
	settingsResetArmed                      // backspace armed the selected row's reset; ⏎ confirms it
)

// settingsPane is the overlay's inline state on the Model. Its zero value is "closed", so it lives
// inline in the value-copied Model like [picker] and [sessionBrowser] (ADR 0011): plain values and
// one slice that is REPLACED rather than appended into (recordEdit), never a self-referential type
// held by value. selected indexes the SETTING rows the provider returns — not the display rows the
// pane paints, which interleave unselectable section headers — and it is clamped rather than trusted,
// because the list underneath it is re-derived on every key and every frame.
//
// edits and failure are the pane's memory of its OWN writes, and they are display-only: they say what
// this pane persisted and what a refusal said, never what the config now holds (the provider answers
// that, from the resolution this run made). sub is the value sub-list's highlight, meaningful only
// while kind is [settingsEnumList]; buf is the string/int edit buffer, meaningful only while kind is
// [settingsValueBuffer], and it is a plain string so the whole pane stays a value the Model can copy.
type settingsPane struct {
	open     bool
	kind     settingsKind
	selected int
	sub      int
	buf      string
	edits    []settingEdit
	failure  settingFailure
}

// settingEdit is one key this pane PERSISTED this session and the value the file now yields for it —
// the fact behind a row's "(next launch)" marker. It is not a cache of the config: the file is
// authoritative and the pane never reads it back, so this is only ever used to say "you changed this,
// here to what".
//
// reset marks the one edit that did not WRITE a value: a reset removed the key's line, so the value
// recorded is the DEFAULT the key went back to (empty when it defaults to unset) rather than anything
// the human typed. The row's marker needs the difference — a reset of a masked key has no secret to
// keep quiet about, and an empty value it returned to is spelled "unset" rather than as a blank.
type settingEdit struct {
	path  string
	value string
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

// settingsTitle names the pane, and the hints are the one-line key legends at its foot — one per
// step, because the keys mean different things in each: in the key list ⏎ opens the selected row's
// edit idiom and esc leaves the pane, while in a value sub-list ⏎ COMMITS the highlighted value and
// esc backs out of the question without writing anything.
// The buffer's legend names the two keys that end it and nothing else — what a caret on a row already
// says is that the row is being typed into — and the reset's is the one line the pane ASKS anything:
// backspace armed something destructive, so the hint is where the confirmation is spelled out, which
// is the /sessions delete-confirm posture with ⏎ in place of y.
const (
	settingsTitle      = "Settings"
	settingsHint       = "↑/↓ select · ⏎ edit · esc close"
	settingsEnumHint   = "↑/↓ select · ⏎ set · esc back"
	settingsBufferHint = "⏎ save · esc cancel"
	settingsResetHint  = "⏎ confirm reset · esc cancel"
)

// settingsCaret is the cell drawn after the edit buffer's text — the /sessions rename idiom's own
// glyph, and the whole of the caret this mini-editor has: the buffer appends and pops at its end, so
// there is no position to move and nothing to draw one at.
const settingsCaret = "▏"

// settingsUnsetValue is how a marker spells a value that is not there — the state a reset returns a
// key with no built-in default to. "" would render as a marker that trailed off ("→  (next launch)"),
// which is the one thing the row must not do after a deliberate act.
const settingsUnsetValue = "unset"

// settingsSavedNote is a masked key's whole marker: the act, with no value behind an arrow. It is the
// note and not a label, because there is nothing for the arrow to point AT — the pane persisted a
// secret it does not hold and will not describe.
const settingsSavedNote = "saved (next launch)"

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
	if m.settings.kind == settingsResetArmed {
		if row, ok := m.settingsResetTarget(rows); ok {
			return m.settingsResetKey(msg, row)
		}
		return m.settingsAbandonStep()
	}
	switch msg.String() {
	case "esc":
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

// settingsAbandonStep drops a second step whose row went away and swallows the keypress that found
// it gone — the enum sub-list's fallback, shared by the buffer and the armed reset. Nothing is
// written and nothing is kept: a buffer whose key is no longer there has nothing to save, and a
// confirmation for a row that left cannot be confirmed.
func (m Model) settingsAbandonStep() (tea.Model, tea.Cmd) {
	m.settings.kind, m.settings.sub, m.settings.buf = settingsKeyList, 0, ""
	m.layout()
	return m, nil
}

// settingsEnter opens the selected row's edit idiom — the one place the pane decides what ⏎ MEANS,
// which is the row's kind and nothing else:
//
//   - a bool is toggled and persisted on the spot, because a two-value key has no question to ask;
//   - an enum asks which value, in a sub-list of its own (the /schedule two-step);
//   - a string or an int opens a buffer on the row, seeded with what the key holds; and
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
		return m, nil
	}
	switch row.Kind {
	case SettingBool:
		return m.settingsWrite(row, toggledSetting(m.settingsPersistedValue(row)))
	case SettingEnum:
		if len(row.EnumValues) == 0 {
			return m, nil // an enum with no vocabulary has nothing to offer (the registry pins this)
		}
		// The sub-list opens ON the value the key holds, not at the top of the list: the human who
		// presses ⏎ twice has then confirmed what was already set — which saveConfigSetting writes
		// nothing for — where a highlight reset to the first row would have silently changed the key.
		m.settings.kind = settingsEnumList
		m.settings.sub = max(0, indexOfSetting(row.EnumValues, m.settingsPersistedValue(row)))
		m.layout()
		return m, nil
	case SettingString, SettingInt:
		m.settings.kind = settingsValueBuffer
		m.settings.buf = m.settingsBufferSeed(row)
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
func (m Model) settingsEnumKey(msg tea.KeyPressMsg, row SettingRow) (tea.Model, tea.Cmd) {
	n := len(row.EnumValues)
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
		value := row.EnumValues[clampInt(m.settings.sub, 0, n-1)]
		m.settings.kind, m.settings.sub = settingsKeyList, 0
		return m.settingsWrite(row, value)
	}
	return m, nil // swallowed, like every key the pane does not act on
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

// settingsBufferKey routes a keypress in the string/int edit buffer — the /sessions rename idiom
// (sessions.go): printable text appends, backspace pops the last RUNE (never the last byte, which
// would leave half a grapheme in the buffer and half a value in the file), ⏎ commits and esc
// abandons. Everything else is swallowed, as everywhere else in this modal pane.
//
// The buffer is the one state in which backspace does not arm a reset: inside an edit it means what
// it means in every other text field on the screen. That is exactly why the two idioms can share the
// key — the pane's kind says which of them is being typed at.
func (m Model) settingsBufferKey(msg tea.KeyPressMsg, row SettingRow) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// An abandoned edit takes its refusal with it: the ✗ on this row is the reason THIS buffer
		// was not accepted, and leaving it up after the human walked away from the edit would report
		// a failure against a row nobody is editing any more.
		m.settings.kind, m.settings.buf = settingsKeyList, ""
		m.settings.failure = settingFailure{}
		m.layout()
		return m, nil
	case "enter":
		return m.settingsCommitBuffer(row)
	case "backspace":
		if r := []rune(m.settings.buf); len(r) > 0 {
			m.settings.buf = string(r[:len(r)-1])
		}
		return m, nil
	}
	if msg.Text != "" { // a printable keypress carries its rune(s) in Text
		m.settings.buf += msg.Text
	}
	return m, nil
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
	value := stripEscapes(strings.TrimSpace(m.settings.buf))
	if value == "" {
		m.settings.kind, m.settings.buf = settingsKeyList, ""
		m.layout()
		return m, nil
	}
	next, landed := m.settingsPersist(row, value)
	m = next
	if landed {
		m.settings.kind, m.settings.buf = settingsKeyList, ""
	}
	m.layout()
	return m, nil
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
// DEFAULT — and `mode`, the one key with a live seam, applies that default now. The armed state ends
// either way: the question was answered.
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
	m = m.settingsApplied(row, settingEdit{path: row.Path, value: row.Default, reset: true})
	m.layout()
	return m, nil
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
//   - landed — the edit is recorded for the row's marker, and `mode` (the one key with a live seam)
//     also takes effect now, through the same Engine.SetMode + opts.Mode pair Shift+Tab drives, so
//     the footer's mode and the Agent's agree with the file the same instant.
func (m Model) settingsWrite(row SettingRow, value string) (tea.Model, tea.Cmd) {
	m, _ = m.settingsPersist(row, value)
	m.layout()
	return m, nil
}

// settingsPersist is settingsWrite's body and its outcome: the model after the attempt, and whether the
// write LANDED. The bool exists for the edit buffer, which is the one caller whose next move depends on
// it — a refused value keeps its buffer open so it can be corrected (settingsCommitBuffer), where a
// refused toggle has nothing to keep. It does not lay out: the caller does, once, after it has finished
// deciding what the pane is now doing.
func (m Model) settingsPersist(row SettingRow, value string) (Model, bool) {
	if m.opts.WriteSetting == nil {
		m.settings.failure = settingFailure{path: row.Path, msg: noSettingsWriterNote}
		return m, false
	}
	if err := m.opts.WriteSetting(row.Path, value); err != nil {
		m.settings.failure = settingFailure{path: row.Path, msg: err.Error()}
		return m, false
	}
	return m.settingsApplied(row, settingEdit{path: row.Path, value: value}), true
}

// settingsApplied records an edit that LANDED and applies it live where a seam exists — the one place
// both halves of "the file now says this" happen, so a write and a reset cannot drift apart on either
// (a reset of `mode` must switch the running session's mode exactly as a write of it does).
//
// The live apply is guarded on a non-empty value as well as on the key: a reset records the key's
// DEFAULT, and a key whose default is unset would otherwise hand the Engine an empty mode — which
// `mode` never is, and which the guard keeps from becoming a possibility a future live key inherits.
func (m Model) settingsApplied(row SettingRow, edit settingEdit) Model {
	m.settings = m.settings.recordEdit(edit)
	if row.Path == settingsModeKey && edit.value != "" {
		mode := domain.Mode(edit.value)
		m.eng.SetMode(mode)
		m.opts.Mode = mode // the footer renders the mode from opts.Mode (footerContent), as Shift+Tab does
	}
	return m
}

// settingsModeKey is the registry path of the one key an edit APPLIES as well as persists. It is
// named here rather than derived from [SettingRow.Restart] because what makes it live is the
// existence of a seam — Engine.SetMode — and not the absence of a restart: a future key that gains
// one gets a line here beside it, which is the honest amount of coupling for a rebind-free live apply
// (ADR 0035: the pane never triggers rebinds; /model and /server own those).
const settingsModeKey = "mode"

// recordEdit returns the pane with edit recorded, replacing any earlier edit of the same key — the
// last one is what the file says, whether it wrote a value or removed the line. The slice is built
// FRESH rather than appended to, the value-copied Model's rule (ADR 0011, doc.go): an append could
// write into an array a Model copy still in flight is sharing, and the copies are not ours to reason
// about.
//
// A landed write also clears the failure slot, which is one attempt's outcome and not one row's
// condition: the human just saw a write succeed, and a refusal left over from a previous keypress
// would go on contradicting it.
func (p settingsPane) recordEdit(edit settingEdit) settingsPane {
	next := make([]settingEdit, 0, len(p.edits)+1)
	for _, e := range p.edits {
		if e.path != edit.path {
			next = append(next, e)
		}
	}
	p.edits = append(next, edit)
	p.failure = settingFailure{}
	return p
}

// editOf is what this pane did to path and whether it did anything at all. A linear scan over at most
// one edit per config key is the right shape here: the list is short, it is read once per row per
// frame, and a map on the pane would be a reference the Model's copies would share.
func (p settingsPane) editOf(path string) (settingEdit, bool) {
	for _, e := range p.edits {
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
	if edit, ok := m.settings.editOf(row.Path); ok {
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
	if !row.Editable || row.Kind != SettingEnum || len(row.EnumValues) == 0 {
		return SettingRow{}, false
	}
	return row, true
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
func (m Model) settingsResettable(row SettingRow) bool {
	if !row.Editable {
		return false
	}
	if _, edited := m.settings.editOf(row.Path); edited {
		return true
	}
	return row.Value != row.Default
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
// section headers interleaved — plus where the selected KEY landed among them. The two travel
// together because they are one composition: inserting a header shifts every row after it, so a
// selection index derived separately would highlight the wrong row the moment a section opened above
// it.
type settingsDisplay struct {
	rows     []popupRow
	selected int // index into rows of the selected key row; −1 when nothing is highlighted
}

// settingsDisplayRows composes the FULL row list the pane paints: a single-cell header row wherever
// the section changes, then one four-cell row per key. Rows arrive in the provider's order, which is
// the config template's order (ADR 0035), so the pane reads in the order the file does and a key
// added to the registry appears where it was added.
//
// Headers are rows the popup module paints and NOT rows the selection can land on. That is why the
// selection indexes the setting rows and this method reports where the highlighted one ended up:
// ↑/↓ walks keys, the headers scroll past with them, and no keypress can leave the ❯ on a label
// there is nothing to do with.
func (m Model) settingsDisplayRows(rows []SettingRow, selected int) settingsDisplay {
	if len(rows) == 0 {
		return settingsDisplay{rows: singleCellRows([]string{noSettingsRow}), selected: -1}
	}
	out := make([]popupRow, 0, len(rows)+len(rows)/4) //nolint:mnd // a header every few keys, at a guess
	display := -1
	section := ""
	for i, row := range rows {
		if row.Section != "" && row.Section != section {
			section = row.Section
			out = append(out, popupRow{stripEscapes(row.Section)})
		}
		if i == selected {
			display = len(out)
		}
		cells := m.settingRowCells(row)
		// An open edit buffer replaces the SELECTED row's value cell, in place: the key stays where it
		// was in the list and in its column, so the human types into the row they chose rather than
		// into a prompt that has covered it (the /sessions rename idiom, one column narrower — a
		// setting's row is a key and a value, and it is the value being typed).
		if i == selected && m.settings.kind == settingsValueBuffer && settingsBufferable(row) {
			cells = m.settingBufferCells(row)
		}
		out = append(out, cells)
	}
	return settingsDisplay{rows: out, selected: display}
}

// settingBufferCells is the row being typed into: the same four columns, with the buffer and its caret
// where the value goes. The value the key HOLDS is deliberately not shown beside it — the buffer opened
// seeded with it (settingsBufferSeed), so what is on the row is what will be written, and a second
// copy of the old value would only make the human wonder which one the ⏎ takes.
//
// The buffer is escape-stripped here like every other cell: it is keystrokes, and a paste is
// keystrokes too — a bracketed paste carrying an OSC 8 opener would otherwise reach the popup module,
// which strips nothing (doc.go).
func (m Model) settingBufferCells(row SettingRow) popupRow {
	return popupRow{
		stripEscapes(row.Path),
		stripEscapes(m.settings.buf) + settingsCaret,
		stripEscapes(settingsSourceMarker(row.Source)),
		stripEscapes(m.settingsNote(row)),
	}
}

// settingsPaneHint is the legend at the pane's foot: one per step, because the keys mean different
// things in each — and the armed reset's is the one that ASKS, which is what makes backspace safe to
// give a destructive act (settingsResetHint). The enum sub-list has its own renderer and its own hint.
func (m Model) settingsPaneHint() string {
	switch m.settings.kind {
	case settingsValueBuffer:
		return settingsBufferHint
	case settingsResetArmed:
		return settingsResetHint
	}
	return settingsHint
}

// settingRowCells is one key's row in the pane's fixed column schema — ["key", "value", "(env)",
// "· edit in config.yaml"] — so the values line up in one column however long the keys beside them
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
// the provider resolved — with one exception, and it is the exception that keeps the column honest: an
// edit this pane applied LIVE (mode) is what the session is running now, while the provider still
// answers with the resolution this run started from. A restart-required edit is not shown here at all;
// it is the row's marker, because the session is still running the old value and the column would
// otherwise claim a change that has not happened yet.
func (m Model) settingsValueCell(row SettingRow) string {
	if edit, ok := m.settings.editOf(row.Path); ok && !row.Restart {
		return edit.value
	}
	return row.Value
}

// settingsNote is the row's last cell: what became of this pane's own writes to the key, else where a
// key it will not write IS edited. In precedence order, because each one outranks what it replaces:
//
//   - a refused write, because the human's last act on this row failed and nothing else about the row
//     matters as much;
//   - nothing at all for an edit that applied LIVE, because settingsValueCell already shows it and
//     there is nothing left to caveat — this case is ahead of the override note deliberately, since a
//     live apply outranks the source that beat the file at resolution time for as long as this run lasts;
//   - a persisted edit the run is not using yet — "→ auto (next launch)" — or, on a row an environment
//     variable or a flag is overriding, the fuller truth: the file was written and something still
//     outranks it; and
//   - the read-only row's pointer, which is the registry's own fact and the only one of these a pane
//     that has written nothing ever paints.
//
// A masked key's marker carries the MASK rather than what was written ([SettingRow] holds no secret,
// and neither does this): "saved" is the whole of what the row has to say about an api-key.
func (m Model) settingsNote(row SettingRow) string {
	if m.settings.failure.path == row.Path && m.settings.failure.msg != "" {
		return "✗ " + m.settings.failure.msg
	}
	edit, edited := m.settings.editOf(row.Path)
	switch {
	case edited && !row.Restart:
		return "" // applied live: the value cell says it
	case edited && row.Source != SettingFromFile:
		return "saved — overridden by " + settingsSourceLabel(row) + " this run"
	case edited && row.Masked && !edit.reset:
		// A written secret is reported as the ACT and never as the value: this package holds no
		// api-key and no mask of one it could put behind an arrow ([SettingRow]), and "saved" is the
		// whole of what the row has to say. A RESET of it falls through — a removed line left nothing
		// to keep quiet about, so it says "unset" like every other emptied key.
		return settingsSavedNote
	case edited:
		return "→ " + settingsPendingLabel(row, edit) + " (next launch)"
	case row.EditPointer != "":
		return "· " + row.EditPointer
	}
	return ""
}

// settingsPendingLabel is what a marker calls the value the next launch will read: what this pane
// wrote, or — after a reset — the default the key went back to, which for a key that defaults to
// nothing is the word for nothing rather than a blank the arrow would trail off into.
func settingsPendingLabel(row SettingRow, edit settingEdit) string {
	switch {
	case edit.reset && row.Default == "":
		return settingsUnsetValue
	case edit.reset:
		return row.Default
	case edit.value == "":
		return settingsUnsetValue
	}
	return edit.value
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

// settingsBody is the pane's one-line body block: what the SELECTED key is for
// ([SettingRow.Desc]). It is the line popupBudget keeps for a body on every pane whether or not the
// pane draws one, so a bodyless full-height pane would hand it back to the transcript — one stray
// conversation row above a screen the human opened to read. Spent on the description it is the
// difference between a list of key names and a list the human can act on.
//
// It is truncated to the pane's inner width HERE rather than left for the module to wrap, because
// the budget it is composed against is exactly one line: a two-line description would spend that
// line on "… (+2 more lines)" and the pane would say nothing at all about the row under the ❯. One
// line, elided in the painter's own measure (truncateToWidth, ADR 0030), is the honest shape of a
// caption on a row.
func (m Model) settingsBody(rows []SettingRow) string {
	sel := m.settingsSelection(len(rows))
	if sel < 0 {
		return ""
	}
	return truncateToWidth(m.th, stripEscapes(rows[sel].Desc), popupInnerWidth(m.th, m.width))
}

// renderSettings paints the open pane through the shared popup module: a titled, bordered pane
// spanning the full window width (m.width, flush with the input box below), the selected key's
// description as its body, the key rows with their section headers, and the key legend. It returns
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
	display := m.settingsDisplayRows(rows, m.settingsSelection(len(rows)))
	body := m.settingsBody(rows)
	// The body's own claim is its real wrapped height (one line, or none when no row is highlighted)
	// rather than a taste, exactly as the ask prompt states its question's (popupFloor): the pane's
	// caption is a line it will use, and claiming one it would not spend would cost the list a row.
	maxBody, maxRows, seated := m.popupBudget(paneSettings, len(display.rows), len(display.rows),
		popupChrome, popupFloor{body: popupBodyLineCount(m.th, body, m.width)})
	if !seated {
		return "" // the frame cannot seat this pane (settingsGiveWayNote says so on the status line)
	}
	return renderPopup(m.th, popupSpec{
		title:       settingsTitle,
		body:        body,
		maxBodyRows: maxBody,
		rows:        display.rows,
		selected:    display.selected,
		hint:        m.settingsPaneHint(),
		maxRows:     maxRows,
	}, m.width)
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
	current := m.settingsPersistedValue(row)
	values := make([]popupRow, 0, len(row.EnumValues))
	for _, value := range row.EnumValues {
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
