package tui

import (
	tea "charm.land/bubbletea/v2"
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
// against a list captured at open. That is what will let an edit persisted in item 7 show up as the
// value the very next frame paints, without the pane holding a copy of the config that could
// disagree with the file.
//
// It is the one pane granted the WHOLE transcript row budget (frameRowPlan, layout.md): 33 keys and
// their section headers are not a choice to scan but a screen to read, so the conversation gives way
// entirely rather than the list scrolling inside eight rows. Nothing else about the frame moves —
// the four-row pane floor, the twelve-row window below which no pane is drawn, the status line, the
// input box and the footer are all where they were — and when the frame cannot seat the pane at all
// the fact goes on the status line (settingsGiveWayNote), the licence layout.md gives every surface
// that disappears.
//
// This item's pane is READ-ONLY: ⏎ is a no-op, and the writer seams that make the hint's "edit" true
// arrive with items 7 and 8. Everything the pane needs in order to say what a row IS — the effective
// value, the override marker, the mask, the "edit in config.yaml" pointer — is already on the row
// (see [SettingRow]), so nothing here formats a config value or decides what may be written.

// settingsPane is the overlay's inline state on the Model. Its zero value is "closed", so it lives
// inline in the value-copied Model like [picker] and [sessionBrowser] (ADR 0011): two plain values,
// nothing self-referential, no slice held across a copy. selected indexes the SETTING rows the
// provider returns — not the display rows the pane paints, which interleave unselectable section
// headers — and it is clamped rather than trusted, because the list underneath it is re-derived on
// every key and every frame.
type settingsPane struct {
	open     bool
	selected int
}

// settingsTitle names the pane, and settingsHint is the one-line key legend at its foot. ⏎ opens the
// edit idiom for the selected row's kind (items 7 and 8); esc closes the pane.
const (
	settingsTitle = "Settings"
	settingsHint  = "↑/↓ select · ⏎ edit · esc close"
)

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
// the highlight, wrapping at both ends (the pickerKey idiom), and esc closes. ⏎ is a no-op in this
// item — the edit idioms it opens are items 7 and 8 — and every other key is SWALLOWED, because the
// pane is modal: a keystroke that fell through to the input box would edit a draft the human cannot
// see behind a full-height pane.
//
// The row count is re-derived and the selection re-clamped on every key rather than once at open,
// the picker's posture: the provider answers from the binary's live resolution, so the list can
// legitimately change under an open pane (item 7's first persisted edit is exactly that), and a
// selection left pointing past the end of a shorter list would be an index panic one keypress later.
func (m Model) settingsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	n := len(m.settingRows())
	m.settings.clampSelection(n)
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
		return m, nil // the edit idioms are items 7 and 8; this pane only reads
	}
	return m, nil // any other key is swallowed by the modal
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
// selection indexes the setting rows and this function reports where the highlighted one ended up:
// ↑/↓ walks keys, the headers scroll past with them, and no keypress can leave the ❯ on a label
// there is nothing to do with.
func settingsDisplayRows(rows []SettingRow, selected int) settingsDisplay {
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
		out = append(out, settingRowCells(row))
	}
	return settingsDisplay{rows: out, selected: display}
}

// settingRowCells is one key's row in the pane's fixed column schema — ["key", "value", "(env)",
// "· edit in config.yaml"] — so the values line up in one column however long the keys beside them
// run, the override marks in the next, and the pointers in the last. A tier a key does not state is
// an EMPTY cell, which still pads, so a key with no override cannot slide the pointer of the row
// under it sideways; a tier NO key states collapses away entirely (layoutPopupRow), which is what
// keeps a config nothing overrides from paying for a marker column.
//
// Every cell is escape-stripped here, at the producer, as the popup module's contract requires
// (doc.go): a value is the CONFIG FILE's text — an endpoint, a host alias, a present command the
// human typed — and a file on this machine is no more trusted than a model's reply. The mask, the
// structured summary and the pointer wording all arrived decided ([SettingRow]); nothing here
// reformats a value or rules on what may be edited.
func settingRowCells(row SettingRow) popupRow {
	pointer := ""
	if row.EditPointer != "" {
		pointer = "· " + stripEscapes(row.EditPointer)
	}
	return popupRow{
		stripEscapes(row.Path),
		stripEscapes(row.Value),
		stripEscapes(settingsSourceMarker(row.Source)),
		pointer,
	}
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
	display := settingsDisplayRows(rows, m.settingsSelection(len(rows)))
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
		hint:        settingsHint,
		maxRows:     maxRows,
	}, m.width)
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
