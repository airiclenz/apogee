package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// /settings — what a committed key does: persist, journal, apply live
// ----------------------------------------------------------------------------
//
// settings.go decides WHICH key a keypress is about and what the row looks like. This file is what
// happens after that decision: the write goes to the file, the edit goes into the session's journal,
// and the value goes into effect on the running session — ADR 0037's validate → persist → apply, in
// that order and in one place, so a key committed on its row, a key returned to its default, and a
// key found changed by a re-read (settingswatcher.go) all land identically.
//
// Three clusters share the file because they are three faces of one act:
//
//   - the ARMED RESET, which is the only commit whose value is "remove the line" — backspace arms it
//     and ⏎ answers, because deleting from a file a human maintains by hand is not a stray keypress;
//   - the WRITE and the LIVE-APPLY ROUTER, which is where a persisted value is turned into an effect:
//     the renderer-owned keys are applied here (settingsApplyLocal) because there is no engine on
//     the other side of them, and every other key goes out through the binary's dispatcher, which is
//     the only thing that owns the schema (ADR 0037 decision 2);
//   - the EDIT JOURNAL, this session's record of what it did to each key — what the row's ` *`
//     marker, the value an edit starts from, and the value a re-opened sub-list opens on are all
//     read off.

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

// settingsReset returns one key to its default through [SettingsHost.Reset] — the key's line REMOVED
// from the file rather than the default written into it (ADR 0035), so the value goes back to being
// described by the binary and documented by its commented example.
//
// The outcomes are settingsWrite's, for the same reasons: no seam wired, or refused, changes nothing
// but the row's own line; a reset that lands is recorded as the edit it is — the key now yields its
// DEFAULT — and that default is applied to the running session now, on the same terms a written
// value is (settingsApplied). The armed state ends either way: the question was answered.
func (m Model) settingsReset(row SettingRow) (tea.Model, tea.Cmd) {
	m.settings.kind = settingsKeyList
	if m.opts.Settings == nil {
		m.settings.failure = settingFailure{path: row.Path, msg: noSettingsWriterNote}
		m.layout()
		return m, nil
	}
	if err := m.opts.Settings.Reset(row.Path); err != nil {
		m.settings.failure = settingFailure{path: row.Path, msg: err.Error()}
		m.layout()
		return m, nil
	}
	m, cmd := m.settingsApplied(row, settingEdit{path: row.Path, value: row.Default, reset: true})
	m.layout()
	return m, cmd
}

// settingsWrite persists one key through [SettingsHost.Write] and records what became of it. It is
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
	if m.opts.Settings == nil {
		m.settings.failure = settingFailure{path: row.Path, msg: noSettingsWriterNote}
		return m, nil, false
	}
	if err := m.opts.Settings.Write(row.Path, value); err != nil {
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
// The apply is guarded on the EDIT rather than on its value alone. A RESET always applies, even when
// the default it records is empty: the line is gone from the file, and that is a change the running
// session has to hear — the empty string is precisely how the dispatcher is told "the file no longer
// sets this key", which it answers with the built-in default a fresh start would have resolved
// (applySettingFor). Skipping it instead left the session on the old value until a restart, with the
// config watcher unable to heal it because the reset refreshes its self-write baseline.
//
// An empty value that is NOT a reset still skips. Nothing this pane WRITES is ever empty — an empty
// buffer commits nothing at all (settingsCommitBuffer) — so the only empty value reaching here
// unreset comes from a re-read that found a key gone (applyReloaded), and that case is left exactly
// as it was.
//
// The Cmd it hands back is the apply's own, and today exactly one key produces one: a colour-scheme
// switch rebuilds every style under a screen already painted in the previous palette, so the frame
// has to be cleared rather than redrawn over (settingsApplyLocal). Every other key returns nil and
// every caller passes whatever comes back on unchanged.
func (m Model) settingsApplied(row SettingRow, edit settingEdit) (Model, tea.Cmd) {
	var applyErr error
	var cmd tea.Cmd
	if edit.reset || edit.value != "" {
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
//   - every other key goes out through [SettingsHost.Apply], the binary's dispatcher, which owns
//     the schema and therefore is the only thing that can turn the file's spelling of a value into
//     whatever the engine seam behind it takes (ADR 0037 decision 2).
//
// `mode` is the one key with a foot in both: the seam moves the Agent, and the footer renders the
// mode from opts.Mode — so the mirror Shift+Tab keeps in step is updated here too, but only once the
// apply has LANDED, or the footer would report an autonomy the engine is not running. An escalation to
// `auto` also answers with a note the seam does not have to know about — the blast radius of the rung
// the ⏎ just took (autoBlastRadiusLine) — because a rung that stops asking is worth a sentence.
// A local apply may also hand back a Cmd and a note of its own, which is why the local branch no
// longer returns an empty note: a colour-scheme switch that loaded with warnings says so on the row
// (settingsApplyLocal) through the same slot "applies at next clear" uses, and asks for the repaint
// its new palette needs. The seam's own keys are unchanged — [SettingsHost.Apply] returns a note
// and never a Cmd, because what it moves is on the far side of the renderer.
func (m Model) settingsApplyLive(path, value string) (Model, string, tea.Cmd, error) {
	if applied, note, cmd, ok, err := m.settingsApplyLocal(path, value); ok {
		return applied, note, cmd, err
	}
	if m.opts.Settings == nil {
		return m, "", nil, nil // no live apply wired: the write stands on its own (ADR 0031's nil-seam degrade)
	}
	note, err := m.opts.Settings.Apply(path, value)
	if err != nil {
		return m, "", nil, err
	}
	if path == settingKeyMode {
		m.opts.Mode = domain.Mode(value) // the footer renders the mode from opts.Mode (footerContent)
		if domain.Mode(value) == domain.ModeAuto {
			// One ⏎ just moved the session to the rung where every model-chosen call runs without a
			// human gate, and the seam answers `mode` with an empty note — so the row says what that
			// means, in /confine's own words. The fence state is read, not decided, here: it is engine
			// state the renderer already renders (confine.go), which is ADR 0011's line.
			note = autoBlastRadiusLine(m.opts.Confinement, m.eng.ConfineToWorkspace())
		}
	}
	return m, note, nil, nil
}

// settingsApplyLocal applies the keys the RENDERER itself owns — the ones whose entire effect is a
// field on this Model — and reports whether the key was one of them. They are named rather than
// derived because what makes a key local is that nothing behind [SettingsHost.Apply] would have
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
	case settingKeyStallAfter:
		after, err := parseStallAfter(value)
		if err != nil {
			return m, "", nil, true, err
		}
		// Nothing is scheduled and nothing is laid out again: the threshold is read where the status
		// line is painted, and the spinner already repaints it every frame while a turn runs.
		m.opts.StallAfter = after
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

// parseStallAfter reads the `ui.stall-after` row's value as the quiet threshold the status line
// waits out, and refuses what a threshold cannot be. It restates the parse internal/config makes at
// startup rather than calling it, because the dependency runs the other way — internal/config
// imports this package — and the whole of the contract is two lines of time.ParseDuration.
//
// The refusal is worded for the row it is rendered on: the key, what the key takes, and the text
// that was offered, with no path in front of it.
func parseStallAfter(value string) (time.Duration, error) {
	after, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || after < 0 {
		return 0, fmt.Errorf("ui.stall-after takes a length of time of 0 or more, like 90s or 2m "+
			"(0 turns the quiet qualifier off), not %q", value)
	}
	return after, nil
}

// applyColorScheme puts a named colour scheme into effect on THIS screen — the live half of ADR
// 0040's settings picker, and the one local apply that rebuilds the whole look rather than moving a
// field.
//
// It re-RESOLVES rather than reading a palette off the Options, so a scheme file the human has just
// edited lands on the next switch: the seam reads the folder every time it is asked
// ([SchemeHost.Resolve]), which is the whole of what apogee offers instead of watching the file.
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
	if m.opts.Schemes == nil {
		return "", nil, errNoSchemeResolver
	}
	s, warnings, ok := m.opts.Schemes.Resolve(name)
	if !ok {
		return "", nil, errNoSchemeResolver
	}
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

// errNoSchemeResolver is what an unwired [SchemeHost.Resolve] costs: the key is persisted and the
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
// settingKeyMechanisms is the one exception to that last sentence and is not applied here at all: it
// is the row whose ⏎ opens a list of its own instead of an editor, and a row can only be recognised
// by its path (settingsEnter, the settingKeyColorScheme precedent in settingsVocabulary).
const (
	settingKeyMode          = "mode"
	settingKeyAutoTitle     = "auto-title"
	settingKeyShowScrollbar = "ui.show-scrollbar"
	settingKeySpinner       = "ui.spinner"
	settingKeySpinnerColor  = "ui.spinner-color"
	settingKeyColorScheme   = "ui.color-scheme"
	settingKeyStallAfter    = "ui.stall-after"
	settingKeyCursorShape   = "cursor-shape"
	settingKeyMechanisms    = "mechanisms"
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
