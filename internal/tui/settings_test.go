package tui

import (
	"fmt"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

// ----------------------------------------------------------------------------
// /settings — the full-height configuration pane (settings.go)
// ----------------------------------------------------------------------------

// settingsTestRows fabricates n setting rows across two sections — the shape the binary's registry
// hands the pane (cmd/apogee/settingsrows.go), reduced to what the renderer reads. The paths and
// values are indexed so a row can be found in a painted frame by name, and every row carries a
// description, as every registry row does.
func settingsTestRows(n int) []SettingRow {
	rows := make([]SettingRow, 0, n)
	for i := range n {
		section := "Upstream"
		if i >= n/2 {
			section = "Interface"
		}
		rows = append(rows, SettingRow{
			Path:     fmt.Sprintf("key-%02d", i),
			Section:  section,
			Kind:     SettingString,
			Value:    fmt.Sprintf("value-%02d", i),
			Default:  "",
			Editable: true,
			Desc:     fmt.Sprintf("what key-%02d is for", i),
		})
	}
	return rows
}

// settingsModel is a ready 80×24 model with a rows provider wired and the pane CLOSED — the state
// /settings is typed into.
func settingsModel(t *testing.T, rows []SettingRow) Model {
	t.Helper()
	opts := testOpts
	opts.SettingsRows = func() []SettingRow { return rows }
	return newTestModelEng(t, &fakeEngine{}, opts)
}

// settingsFrameModel is a model of the given size with the pane already OPEN over n fabricated rows
// — the state every frame-level and painted property below asserts against. One row's value carries
// the grapheme the two width measures disagree about, so a pane composed in one measure and painted
// in the other cannot pass by luck (ADR 0030).
func settingsFrameModel(t *testing.T, width, height, n int) Model {
	t.Helper()
	rows := settingsTestRows(n)
	rows[1].Value = "danger " + vs16Warning
	opts := Options{Workspace: "/ws/a", SettingsRows: func() []SettingRow { return rows }}
	m := modelWithOverlayRoomAt(t, width, height, opts)
	m.settings = settingsPane{open: true}
	m.layout()
	return m
}

// openSettingsPane opens the pane the way the human does: the whole-line verb, submitted.
func openSettingsPane(t *testing.T, m Model) Model {
	t.Helper()
	m.input.SetValue("/settings")
	m = step(t, m, keyEnter())
	if !m.settings.open {
		t.Fatalf("/settings did not open the pane; notes = %+v", m.transcript.entries)
	}
	return m
}

// /settings is synchronous and idle-safe like /sessions: it opens the pane, launches no worker, and
// empties the box the verb was typed into.
func TestSettingsCommandOpensThePane(t *testing.T) {
	typed := settingsModel(t, settingsTestRows(6))
	typed.input.SetValue("/settings")
	m, cmd := stepCmd(t, typed, keyEnter())

	if !m.settings.open {
		t.Fatalf("the pane is closed after /settings; notes = %+v", m.transcript.entries)
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle (/settings must not launch a worker)", m.state)
	}
	if cmd != nil {
		t.Error("/settings returned a Cmd; it drives no worker and no off-loop read")
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("input not cleared: %q", v)
	}
	if m.settings.selected != 0 {
		t.Errorf("selection opens at row %d, want the first row", m.settings.selected)
	}
	if pane := m.renderSettings(); !strings.Contains(strip(pane), "key-00") {
		t.Errorf("the pane does not list the first key:\n%s", strip(pane))
	}
}

// An unwired provider is the nil-seam degrade every other Options provider takes: one honest note
// and NO overlay. A modal pane with nothing in it would be worse than the sentence explaining it —
// and it would swallow every key while saying nothing.
func TestSettingsCommandWithoutRowsNotesAndOpensNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []SettingRow // nil provider when unwired below
		wire bool
	}{
		{"no provider wired", nil, false},
		{"provider with no rows", []SettingRow{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := testOpts
			if tc.wire {
				opts.SettingsRows = func() []SettingRow { return tc.rows }
			}
			m := newTestModelEng(t, &fakeEngine{}, opts)
			m.input.SetValue("/settings")
			m = step(t, m, keyEnter())

			if m.settings.open {
				t.Fatal("the pane opened with no rows to show")
			}
			if got := lastNote(m); got != noSettingsNote {
				t.Errorf("note = %q, want %q", got, noSettingsNote)
			}
			if pane := m.renderSettings(); pane != "" {
				t.Errorf("a closed pane rendered %q", strip(pane))
			}
		})
	}
}

// ↑/↓ walk the KEYS and wrap at both ends (the pickerKey idiom), and every key the pane does not act
// on is swallowed: the pane is modal, and the input box behind a full-height pane is a box the human
// cannot read.
func TestSettingsPaneNavigationWrapsAndSwallowsEveryOtherKey(t *testing.T) {
	rows := settingsTestRows(4)
	m := openSettingsPane(t, settingsModel(t, rows))

	m = step(t, m, keyUp())
	if want := len(rows) - 1; m.settings.selected != want {
		t.Errorf("↑ from the first row selected %d, want a wrap to %d", m.settings.selected, want)
	}
	m = step(t, m, keyDown())
	if m.settings.selected != 0 {
		t.Errorf("↓ from the last row selected %d, want a wrap to 0", m.settings.selected)
	}
	m = step(t, m, keyDown())
	if m.settings.selected != 1 {
		t.Errorf("↓ selected %d, want 1", m.settings.selected)
	}

	// A printable key neither edits the draft nor moves the selection, and ⏎ is a no-op in this
	// read-only pane (the edit idioms are items 7 and 8 of the settings-screen plan).
	m = step(t, m, keyRune('x'))
	m, cmd := stepCmd(t, m, keyEnter())
	if v := m.input.Value(); v != "" {
		t.Errorf("a key reached the input box behind the pane: %q", v)
	}
	if cmd != nil {
		t.Error("⏎ in the read-only pane returned a Cmd; it must do nothing at all")
	}
	if !m.settings.open || m.settings.selected != 1 {
		t.Errorf("pane = %+v, want it open and still on row 1", m.settings)
	}
}

// esc is the way out, and it closes the pane whole: the next frame is the conversation again.
func TestSettingsPaneEscCloses(t *testing.T) {
	m := step(t, openSettingsPane(t, settingsModel(t, settingsTestRows(6))), keyEsc())

	if m.settings != (settingsPane{}) {
		t.Errorf("pane = %+v, want the zero value (closed)", m.settings)
	}
	if pane := m.renderSettings(); pane != "" {
		t.Errorf("a closed pane still rendered:\n%s", strip(pane))
	}
	if got := m.frameOverlays().settings; got != "" {
		t.Errorf("the frame still stacks the pane's block:\n%s", strip(got))
	}
}

// The rows are DERIVED on every key and every frame, so a list that SHRANK under the open pane
// clamps the selection instead of indexing past its end — the picker's own posture, and here the
// list is the binary's live resolution (item 7 persists edits under this very pane).
func TestSettingsPaneClampsSelectionToRowsThatShrank(t *testing.T) {
	rows := settingsTestRows(6)
	opts := testOpts
	opts.SettingsRows = func() []SettingRow { return rows }
	m := openSettingsPane(t, newTestModelEng(t, &fakeEngine{}, opts))

	for range 5 {
		m = step(t, m, keyDown())
	}
	if m.settings.selected != 5 {
		t.Fatalf("selection = %d, want the last of six rows", m.settings.selected)
	}

	rows = settingsTestRows(2) // the provider now answers with a shorter list
	m = step(t, m, keyDown())
	if m.settings.selected >= 2 {
		t.Errorf("selection = %d, want it clamped inside the two rows that are left", m.settings.selected)
	}
	if pane := strip(m.renderSettings()); !strings.Contains(pane, "key-01") {
		t.Errorf("the pane does not list the shorter offering:\n%s", pane)
	}
}

// The row schema: a single-cell header wherever the section changes, then one row per key with its
// value, its override marker and its read-only pointer each in a column of its own (the Column
// contract, layout.md). The selection indexes KEYS and the display index follows the headers, so the
// ❯ can never land on a label there is nothing to do with.
func TestSettingsDisplayRowsInterleaveSectionHeaders(t *testing.T) {
	rows := []SettingRow{
		{Path: "endpoint", Section: "Upstream", Value: "http://h:1111", Editable: true},
		{Path: "mode", Section: "Upstream", Value: "auto", Editable: true,
			Source: SettingFromEnv, SourceName: "APOGEE_MODE"},
		{Path: "servers", Section: "Upstream", Value: "3 servers", EditPointer: "edit in config.yaml"},
		{Path: "ui.spinner", Section: "Interface", Value: "snake", Editable: true},
	}

	got := settingsDisplayRows(rows, 3)

	want := []popupRow{
		{"Upstream"},
		{"endpoint", "http://h:1111", "", ""},
		{"mode", "auto", "(env)", ""},
		{"servers", "3 servers", "", "· edit in config.yaml"},
		{"Interface"},
		{"ui.spinner", "snake", "", ""},
	}
	if len(got.rows) != len(want) {
		t.Fatalf("display rows = %q, want %q", got.rows, want)
	}
	for i := range want {
		if strings.Join(got.rows[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("row %d = %q, want %q", i, got.rows[i], want[i])
		}
	}
	if want := 5; got.selected != want {
		t.Errorf("selected display row = %d, want %d — the headers above it shift it down", got.selected, want)
	}
}

// A row's cells are escape-stripped at the producer, as the popup module's contract requires: a
// config value is a FILE's text, and a file on this machine is no more trusted than a model's reply
// (doc.go). One unterminated OSC 8 opener would otherwise turn the rest of the frame into a link.
func TestSettingsRowCellsStripEscapes(t *testing.T) {
	row := SettingRow{
		Path:        "host\x1b]8;;http://evil\x1b\\-alias",
		Value:       "one\x1bctwo",
		EditPointer: "edit \x1b[31min config.yaml",
	}

	for i, cell := range settingRowCells(row) {
		if strings.ContainsRune(cell, 0x1b) {
			t.Errorf("cell %d carries an ESC byte: %q", i, cell)
		}
	}
}

// The full-height rule: while the pane is open the transcript keeps NO rows at all, and the pane is
// as tall as the whole budget the frame had to give — not the eight-row window the picker and the
// browser cap themselves at (ratified call 1 of the settings-screen plan).
func TestSettingsPaneClaimsTheWholeTranscriptBudget(t *testing.T) {
	m := settingsFrameModel(t, 80, 24, 40)

	ov := m.frameOverlays()
	budget := m.transcriptBudget()
	if got := lipgloss.Height(ov.settings); got != budget {
		t.Errorf("the pane is %d rows of the %d the frame had to give:\n%s", got, budget, strip(ov.settings))
	}
	if got := ov.transcriptRows(budget); got != 0 {
		t.Errorf("the transcript kept %d rows under a full-height pane, want none", got)
	}
	if got := len(strings.Split(strip(m.View().Content), "\n")); got != m.height {
		t.Errorf("composed frame is %d rows on a %d-row terminal", got, m.height)
	}
	// Well past maxPickerRows: the pane asks for every row it has and the budget is the only cap.
	shown := strings.Count(strip(ov.settings), "key-")
	if shown <= maxPickerRows {
		t.Errorf("the pane showed %d key rows, want more than the picker's %d-row taste:\n%s",
			shown, maxPickerRows, strip(ov.settings))
	}
}

// D2, for the frame's first full-height pane: it never promises rows the frame cannot hold. The pane
// takes the transcript's reserve, and NOTHING else moves — the band keeps its shape, the box keeps
// its rows, and the composed frame fits every window the pane can be drawn in at all.
func TestSettingsPaneFitsEveryWindowItIsDrawnIn(t *testing.T) {
	for _, staged := range []int{0, 5} {
		for _, width := range []int{80, narrowOverlayWindow} {
			for _, height := range []int{8, 10, 11, smallestOverlayWindow, 13, 14, 16, 20, 24, 30} {
				t.Run(fmt.Sprintf("%d staged/%d×%d", staged, width, height), func(t *testing.T) {
					m := withStagedRows(settingsFrameModel(t, width, height, 40), staged)

					plainFrame := strip(m.View().Content)
					frame := strings.Split(plainFrame, "\n")
					if len(frame) > max(height, frameFloorRows) {
						t.Errorf("composed frame is %d rows on a %d-row terminal (+%d):\n%s",
							len(frame), height, len(frame)-height, plainFrame)
					}
					if last := strings.TrimSpace(frame[len(frame)-1]); last == "" {
						t.Errorf("last frame row is blank, want the ▁ bottom-edge hairline:\n%s", plainFrame)
					}
					// Seated or not, the frame says the pane is open: the pane's own title where it was
					// drawn, the status line's give-way note where it was not.
					pane := m.frameOverlays().settings
					switch {
					case pane == "" && !strings.Contains(plainFrame, settingsGiveWayNote):
						t.Errorf("the pane gave way with nothing on the status line:\n%s", plainFrame)
					case pane != "" && !strings.Contains(plainFrame, settingsTitle):
						t.Errorf("the pane is drawn but does not name itself:\n%s", plainFrame)
					}
					// A drawn pane showing no rows owes the count, exactly as every other pane does
					// (popupTitleLine): the window that seats none is the case the scroll cannot answer.
					if pane != "" && !strings.Contains(plainFrame, "key-00") &&
						!elisionMarkerPattern.MatchString(plainFrame) {
						t.Errorf("pane hid every one of its rows with no marker anywhere:\n%s", plainFrame)
					}
				})
			}
		}
	}
}

// A pane that gives way entirely leaves its fact on the status line (layout.md) — and for this pane
// the fact has to carry the way OUT: it is swallowing every key on a window that is showing none of
// it, so a frame that looked idle would leave the human with a dead keyboard.
func TestSettingsGiveWayLeavesItsFactOnTheStatusLine(t *testing.T) {
	// Eleven rows: three of transcript budget, one short of the four an honest pane needs.
	m := settingsFrameModel(t, 80, 11, 40)

	if m.settingsSeated() {
		t.Fatal("the pane is seated at eleven rows — test premise broken")
	}
	if got := m.frameOverlays().settings; got != "" {
		t.Errorf("an unseated pane rendered:\n%s", strip(got))
	}
	if got := strip(m.statusLine()); !strings.Contains(got, settingsGiveWayNote) {
		t.Errorf("status line = %q, want it to carry %q", got, settingsGiveWayNote)
	}

	// And it is the OPEN pane's fact alone: a closed one leaves the idle slot as empty as it was.
	m.settings = settingsPane{}
	if got := strip(m.statusLine()); strings.Contains(got, settingsGiveWayNote) {
		t.Errorf("status line = %q with the pane closed, want no give-way note", got)
	}
}
