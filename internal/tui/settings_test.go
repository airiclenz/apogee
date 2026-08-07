package tui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// keyBackspace is the rune-pop of an edit buffer and the arming key of a reset — the one keypress the
// settings pane gives two meanings, told apart by what the pane is doing (settingsKind).
func keyBackspace() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyBackspace} }

// typeSetting sends text into an open edit buffer one printable keypress at a time, the way a human
// fills it.
func typeSetting(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m = step(t, m, keyRune(r))
	}
	return m
}

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

	// A printable key in the key list neither edits the draft nor moves the selection, and ⏎ on a
	// STRING row opens its edit buffer on the spot — driving no Cmd either way: every one of the
	// pane's own acts is synchronous.
	m = step(t, m, keyRune('x'))
	m, cmd := stepCmd(t, m, keyEnter())
	if v := m.input.Value(); v != "" {
		t.Errorf("a key reached the input box behind the pane: %q", v)
	}
	if cmd != nil {
		t.Error("a keypress in the pane returned a Cmd; it drives nothing off the loop")
	}
	if !m.settings.open || m.settings.selected != 1 || m.settings.kind != settingsValueBuffer {
		t.Errorf("pane = %+v, want it open on row 1 with the row's edit buffer", m.settings)
	}
}

// esc is the way out, and it closes the pane whole: the next frame is the conversation again.
func TestSettingsPaneEscCloses(t *testing.T) {
	m := step(t, openSettingsPane(t, settingsModel(t, settingsTestRows(6))), keyEsc())

	// DeepEqual rather than ==: the pane carries the edit field's widget now, which is not a
	// comparable type. The claim is unchanged — esc leaves the pane at exactly its zero value.
	if !reflect.DeepEqual(m.settings, settingsPane{}) {
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

// The row schema: a blank spacer and a single-cell header wherever the section changes, then one row
// per key with its value, its override marker and its read-only pointer each in a column of its own
// (the Column contract, layout.md). The selection indexes KEYS and the display index follows the
// labels, so the ❯ can never land on one there is nothing to do with — and each display row carries
// what it IS (popupRowKind), which is how the painter tells a section label from the keys under it.
//
// The FIRST section takes no spacer: the description header above the list closes with a blank line
// of its own (settingsBody), and a second would open the pane on a gap.
func TestSettingsDisplayRowsInterleaveSectionHeaders(t *testing.T) {
	rows := []SettingRow{
		{Path: "endpoint", Section: "Upstream", Value: "http://h:1111", Editable: true},
		{Path: "mode", Section: "Upstream", Value: "auto", Editable: true,
			Source: SettingFromEnv, SourceName: "APOGEE_MODE"},
		{Path: "servers", Section: "Upstream", Value: "3 servers", EditPointer: "⏎ opens $EDITOR", ExternalEdit: true},
		{Path: "ui.spinner", Section: "Interface", Value: "snake", Editable: true},
	}

	got := Model{}.settingsDisplayRows(rows, 3)

	want := []popupRow{
		{"Upstream"},
		{"endpoint", "http://h:1111", "", ""},
		{"mode", "auto", "(env)", ""},
		{"servers", "3 servers", "", "· ⏎ opens $EDITOR"},
		{""},
		{"Interface"},
		{"ui.spinner", "snake", "", ""},
	}
	wantKinds := []popupRowKind{
		popupRowHeading, popupRowPlain, popupRowPlain, popupRowPlain,
		popupRowPlain, popupRowHeading, popupRowPlain,
	}
	if len(got.rows) != len(want) {
		t.Fatalf("display rows = %q, want %q", got.rows, want)
	}
	for i := range want {
		if strings.Join(got.rows[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("row %d = %q, want %q", i, got.rows[i], want[i])
		}
	}
	if !reflect.DeepEqual(got.kinds, wantKinds) {
		t.Errorf("row kinds = %v, want %v", got.kinds, wantKinds)
	}
	if want := 6; got.selected != want {
		t.Errorf("selected display row = %d, want %d — the labels above it shift it down", got.selected, want)
	}
}

// The description header is the pane's own first block: the "Description:" label, what the key under
// the ❯ is FOR beside it, and a blank line closing it off from the list
// (docs/layout/settings-screen-layout.md). It follows the selection, and it is a FIXED-height region
// — a one-line description and a four-line one compose the same number of lines — so walking the
// list moves the highlight and nothing else (ADR 0037 decision 9). What a long description loses is
// its tail, to an ellipsis, rather than the list losing a row to it.
func TestSettingsPaneDescriptionHeaderNamesTheSelectedRowAtAFixedHeight(t *testing.T) {
	rows := settingsTestRows(4)
	rows[0].Desc = "Short."
	rows[1].Desc = strings.Repeat("a description with plenty to say for itself ", 6)
	m := openSettingsPane(t, settingsModel(t, rows))

	first := strings.Split(m.settingsBody(rows), "\n")
	if want := settingsDescLabel + " " + rows[0].Desc; first[0] != want {
		t.Errorf("header line = %q, want %q", first[0], want)
	}
	if want := settingsDescLines + 1; len(first) != want {
		t.Errorf("a one-line description composed %d header lines, want the fixed %d", len(first), want)
	}
	if last := first[len(first)-1]; last != "" {
		t.Errorf("the header does not end on its blank line: %q", last)
	}
	if pane := strip(m.renderSettings()); !strings.Contains(pane, settingsDescLabel+" Short.") {
		t.Errorf("the pane does not paint the header:\n%s", pane)
	}
	if colorActive(m.th) {
		if got := m.renderSettings(); !strings.Contains(got, styleSGR(m.th.popupBodyLead)) {
			t.Errorf("the %q label is not painted as a heading:\n%s", settingsDescLabel, strip(got))
		}
	}

	moved := step(t, m, keyDown())
	second := strings.Split(moved.settingsBody(rows), "\n")
	if want := settingsDescLines + 1; len(second) != want {
		t.Fatalf("a long description composed %d header lines, want the same fixed %d", len(second), want)
	}
	if tail := second[settingsDescLines-1]; !strings.HasSuffix(tail, "…") {
		t.Errorf("the region's last line = %q, want the overflow elided into it", tail)
	}
	if strings.Contains(strip(moved.renderSettings()), settingsDescLabel+" Short.") {
		t.Errorf("the header still describes the row above the selection:\n%s", strip(moved.renderSettings()))
	}
	// The fixed height is what the list is spared: the same keys are on the screen either way.
	if a, b := strings.Count(strip(m.renderSettings()), "key-"), strings.Count(strip(moved.renderSettings()), "key-"); a != b {
		t.Errorf("the list jumped from %d rows to %d when the description changed length", a, b)
	}
}

// The pane names itself in BOLD (docs/layout/settings-screen-layout.md's **SETTINGS**) — and it does
// so through the shared chrome every popup's title is painted with (th.presentTitle) rather than
// through anything of its own. That is the point of pinning it here: a requirement met by the module
// is one this pane cannot drift away from, and the guard says which style carries it.
func TestSettingsPaneTitleIsPaintedBold(t *testing.T) {
	m := settingsFrameModel(t, 80, 30, 6)

	if !colorActive(m.th) {
		t.Skip("no colour on this profile: the weight is the whole claim")
	}
	line := popupLineWith(t, m.renderSettings(), settingsTitle)
	if !strings.Contains(line, styleSGR(m.th.presentTitle)) {
		t.Errorf("the pane's title is not painted as a heading: %q", line)
	}
}

// The sections read as sections: a blank line above each label after the first, the label itself in
// white where the rows under it are faint (docs/layout/settings-screen-layout.md requirements 3 and
// 4). The spacer is a display row like any other — it scrolls with the list — and no keypress can
// land the selection on either.
func TestSettingsPaneSectionsAreSpacedAndLabelledInWhite(t *testing.T) {
	m := settingsFrameModel(t, 80, 40, 8)

	lines := popupLines(m.renderSettings())
	label := -1
	for i, ln := range lines {
		if popupInterior(ln) == "Interface" {
			label = i
			break
		}
	}
	if label < 1 {
		t.Fatalf("no section label on the pane:\n%s", strip(m.renderSettings()))
	}
	if above := popupInterior(lines[label-1]); above != "" {
		t.Errorf("the row above the section label is %q, want the spacer", above)
	}
	if !colorActive(m.th) {
		return // the styling is the rest of the claim, and this profile emits none
	}
	if !strings.Contains(lines[label], styleSGR(m.th.popupHeading)) {
		t.Errorf("the section label is not painted white: %q", lines[label])
	}
	if strings.Contains(lines[label], styleSGR(m.th.statusFaint)) {
		t.Errorf("the section label is still painted as a faint row: %q", lines[label])
	}
}

// The row being EDITED says so in colour (docs/layout/settings-screen-layout.md requirement 2): the
// buffer's row carries the edit tone rather than the plain selection bar, and the pane knows it is
// editing whichever second step holds a row — the buffer typed into on the row, or the enum
// sub-list answering about it.
func TestSettingsPaneLightsTheRowItIsEditing(t *testing.T) {
	rows := []SettingRow{settingsStringRow(), settingsEnumRow()}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	if display := m.settingsDisplayRows(rows, 0); display.kinds[display.selected] != popupRowPlain {
		t.Fatalf("a row nothing is editing has kind %v, want it plain", display.kinds[display.selected])
	}

	m = step(t, m, keyEnter())

	display := m.settingsDisplayRows(rows, 0)
	if got := display.kinds[display.selected]; got != popupRowEditing {
		t.Errorf("the buffered row has kind %v, want %v", got, popupRowEditing)
	}
	if !m.settingsEditing(rows[0]) || m.settingsEditing(rows[1]) {
		t.Error("settingsEditing does not name the buffered row alone")
	}
	if colorActive(m.th) {
		line := popupLineWith(t, m.renderSettings(), rows[0].Path)
		if !strings.Contains(line, styleSGR(m.th.popupEdit)) {
			t.Errorf("the edited row is not lit: %q", line)
		}
		if strings.Contains(line, styleSGR(m.th.userBlock)) {
			t.Errorf("the edited row still carries the plain selection bar: %q", line)
		}
	}

	// The enum's second step holds a row too, and it is the parent row that is being edited.
	enum := step(t, step(t, m, keyEsc()), keyDown())
	if enum = step(t, enum, keyEnter()); enum.settings.kind != settingsEnumList {
		t.Fatalf("pane = %+v, want the value sub-list open", enum.settings)
	}
	if !enum.settingsEditing(rows[1]) {
		t.Error("the enum sub-list's parent row does not read as the row being edited")
	}
}

// The header and the legend are the pane's, and the LIST is what gives way: at every window the pane
// is drawn in the legend is on the screen and the description block keeps its lines ahead of the
// rows — and where the frame is too short to seat the whole region, what it dropped is counted
// (popupElisionMarker) rather than dropped quietly.
func TestSettingsPaneSeatsItsHeaderAndLegendAtEveryHeight(t *testing.T) {
	for _, height := range []int{smallestOverlayWindow, 13, 14, 16, 20, 24, 30} {
		t.Run(fmt.Sprintf("%d rows", height), func(t *testing.T) {
			pane := strip(settingsFrameModel(t, 80, height, 40).frameOverlays().settings)
			if pane == "" {
				return // the pane gave way whole; the status line carries that fact (its own test)
			}
			if !strings.Contains(pane, settingsHint) {
				t.Errorf("the pane dropped its legend:\n%s", pane)
			}
			if !strings.Contains(pane, settingsDescLabel) && !elisionMarkerPattern.MatchString(pane) {
				t.Errorf("the pane dropped its description header with no accounting:\n%s", pane)
			}
		})
	}

	// With rows to spare the whole region is seated — label, description, blank — and the list
	// scrolls under it.
	pane := strip(settingsFrameModel(t, 80, 30, 40).frameOverlays().settings)
	if want := settingsDescLabel + " what key-00 is for"; !strings.Contains(pane, want) {
		t.Errorf("the header does not describe the selected row (%q):\n%s", want, pane)
	}
	for _, want := range []string{"key-00", settingsHint} {
		if !strings.Contains(pane, want) {
			t.Errorf("the pane does not show %q:\n%s", want, pane)
		}
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

	for i, cell := range (Model{}).settingRowCells(row) {
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

// ----------------------------------------------------------------------------
// Editing: bool toggle and enum sub-list, persisted per edit (ADR 0035)
// ----------------------------------------------------------------------------

// settingsWriteLog is the binary's write half, faked: what the pane asked to be persisted, in order,
// plus the error the next write is refused with. A log is the whole of what this fake needs to be —
// the pane never reads a value back, so there is no file for it to stand in for.
type settingsWriteLog struct {
	writes []settingEdit
	resets []string
	err    error

	// The apply half of the same keypress (ADR 0037): the dispatcher the binary wires behind
	// [Options.ApplySetting], as a spy. applies records every key routed OUT of the renderer — the
	// renderer-owned keys never reach it — note is the boundary sentence the seam hands back, and
	// applyErr is an apply that failed AFTER the write landed.
	applies   []settingEdit
	applyNote string
	applyErr  error
}

// write and reset are the two [Options] seams. A refusal records nothing, exactly as a real refused
// splice writes nothing: the assertion "the file is unchanged" is then the log's own emptiness.
func (l *settingsWriteLog) write(path, value string) error {
	if l.err != nil {
		return l.err
	}
	l.writes = append(l.writes, settingEdit{path: path, value: value})
	return nil
}

func (l *settingsWriteLog) reset(path string) error {
	if l.err != nil {
		return l.err
	}
	l.resets = append(l.resets, path)
	return nil
}

// apply is the third seam. The ATTEMPT is recorded whether or not it succeeds, because that is the
// question the failure case asks: the apply was made, the write already stood, and the row has to
// say so.
func (l *settingsWriteLog) apply(path, value string) (string, error) {
	l.applies = append(l.applies, settingEdit{path: path, value: value})
	if l.applyErr != nil {
		return "", l.applyErr
	}
	return l.applyNote, nil
}

// settingsEditModel is a model with the pane OPEN over rows and all three settings seams wired to
// log — the state every edit flow below starts from. The engine comes back too, for the flows that
// have something to assert about it.
func settingsEditModel(t *testing.T, rows []SettingRow, log *settingsWriteLog) (Model, *fakeEngine) {
	t.Helper()
	eng := &fakeEngine{}
	opts := testOpts
	opts.SettingsRows = func() []SettingRow { return rows }
	opts.WriteSetting = log.write
	opts.ResetSetting = log.reset
	opts.ApplySetting = log.apply
	return openSettingsPane(t, newTestModelEng(t, eng, opts)), eng
}

// settingsBoolRow is one editable bool row — `auto-title:` as the registry describes it
// (cmd/apogee/registry.go), a key the renderer itself applies.
func settingsBoolRow() SettingRow {
	return SettingRow{
		Path: "auto-title", Section: "Session", Kind: SettingBool, Value: "true", Default: "true",
		Editable: true, Desc: "Name a new session from its first prompt.",
	}
}

// settingsEnumRow is one editable enum row — `ui.spinner:` and its closed vocabulary, which is the
// REAL one this build animates (ParseSpinnerStyle): the key applies live in the renderer itself, so
// a fixture vocabulary would be a value the apply then had to refuse.
func settingsEnumRow() SettingRow {
	return SettingRow{
		Path: "ui.spinner", Section: "Interface", Kind: SettingEnum, Value: "snake", Default: "snake",
		EnumValues: []string{"snake", "glitter", "classic"}, Editable: true,
		Desc: "Which animation paints the status-line spinner.",
	}
}

// ⏎ on a bool toggles it and persists the toggle immediately — no second question, because a two-value
// key has none to ask. What was written is APPLIED on the same keypress (ADR 0037 decision 1), so the
// row shows it with the ` *` that says this session changed it; a second ⏎ toggles back from what was
// WRITTEN, not from the resolution the pane opened over.
func TestSettingsPaneTogglesABoolAndPersistsIt(t *testing.T) {
	rows := []SettingRow{settingsBoolRow()}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter())

	if want := []settingEdit{{path: "auto-title", value: "false"}}; !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v", log.writes, want)
	}
	if got, want := m.settingsValueCell(rows[0]), "false"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q — the value the session is running, marked as changed here", got, want)
	}
	if got := m.settingsNote(rows[0]); got != "" {
		t.Errorf("marker = %q, want none: an applied edit has nothing to caveat", got)
	}
	if pane := strip(m.renderSettings()); !strings.Contains(pane, "false"+settingsEditMarker) {
		t.Errorf("the pane does not show the edited value and its marker:\n%s", pane)
	}

	m = step(t, m, keyEnter())

	want := []settingEdit{{path: "auto-title", value: "false"}, {path: "auto-title", value: "true"}}
	if !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want the second ⏎ to toggle back from what was written: %+v", log.writes, want)
	}
	if got, want := m.settingsValueCell(rows[0]), "true"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q — one edit per key, the last one", got, want)
	}
}

// ⏎ on an enum asks WHICH value in a sub-list of its own (the /schedule two-step): the pane keeps its
// place in the frame, the vocabulary replaces the key list, ⏎ commits the highlighted value and esc
// backs out having written nothing. The sub-list opens on the value the key already holds, so a human
// who presses ⏎ twice confirms what was set instead of silently changing it.
func TestSettingsPaneEnumSubListCommitsAndBacksOut(t *testing.T) {
	rows := []SettingRow{settingsEnumRow()}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	opened := step(t, m, keyEnter())

	if opened.settings.kind != settingsEnumList || opened.settings.sub != 0 {
		t.Fatalf("pane = %+v, want the sub-list open on the current value (row 0)", opened.settings)
	}
	pane := strip(opened.renderSettings())
	for _, want := range []string{"ui.spinner", "snake", "glitter", "classic", "(current)", settingsEnumHint} {
		if !strings.Contains(pane, want) {
			t.Errorf("the value sub-list does not show %q:\n%s", want, pane)
		}
	}

	// esc backs out of the QUESTION, not out of the pane, and writes nothing.
	backed := step(t, opened, keyEsc())
	if !backed.settings.open || backed.settings.kind != settingsKeyList || backed.settings.sub != 0 {
		t.Errorf("pane = %+v after esc, want it open again on its key list", backed.settings)
	}
	if len(log.writes) != 0 {
		t.Errorf("esc persisted %+v; backing out must write nothing", log.writes)
	}
	if list := strip(backed.renderSettings()); !strings.Contains(list, settingsHint) {
		t.Errorf("the key list did not come back:\n%s", list)
	}

	// ⏎ straight away commits the value the key already holds — the confirmation the writer no-ops.
	if confirmed := step(t, opened, keyEnter()); confirmed.settings.kind != settingsKeyList {
		t.Errorf("pane = %+v after ⏎, want the sub-list closed", confirmed.settings)
	}
	if want := []settingEdit{{path: "ui.spinner", value: "snake"}}; !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v — the highlight opens on what is set", log.writes, want)
	}

	// And a moved highlight commits what it points at.
	log.writes = nil
	committed := step(t, step(t, opened, keyDown()), keyEnter())
	if want := []settingEdit{{path: "ui.spinner", value: "glitter"}}; !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v", log.writes, want)
	}
	if got, want := committed.settingsValueCell(rows[0]), "glitter"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q", got, want)
	}
	// The spinner is a key the RENDERER owns: it moved here, and the apply seam was never asked.
	if committed.spin.style != SpinnerGlitter {
		t.Errorf("spinner style = %q, want glitter — the commit changed what paints", committed.spin.style)
	}
	if len(log.applies) != 0 {
		t.Errorf("applies = %+v, want none: a renderer-owned key never leaves the renderer", log.applies)
	}
}

// settingsServerRow is the `server:` row as the registry describes it (cmd/apogee/registry.go): a
// row picked from a sub-list like an enum, but with no vocabulary of its own — what it may hold is
// whatever [Options.Servers] answers with — and whose ⏎ is the `/server` switch rather than a write.
func settingsServerRow() SettingRow {
	return SettingRow{
		Path: "server", Section: "Upstream", Kind: SettingServer, Value: "test-host",
		Editable: true, Desc: "Which servers: entry a session starts on.",
	}
}

// settingsServerModel is the pane OPEN over that one row with the upstream seams wired: the live
// list of switchable servers and the spy that records every move it is asked to make. The three
// settings seams are wired too, so a test can prove this row reaches none of them.
func settingsServerModel(t *testing.T, servers func() []ServerChoice, sw *fakeSwitch, log *settingsWriteLog) Model {
	t.Helper()
	rows := []SettingRow{settingsServerRow()}
	opts := testOpts
	opts.SettingsRows = func() []SettingRow { return rows }
	opts.WriteSetting, opts.ResetSetting, opts.ApplySetting = log.write, log.reset, log.apply
	opts.Servers, opts.SwitchServer = servers, sw.switchTo
	return openSettingsPane(t, newTestModelEng(t, &fakeEngine{}, opts))
}

// ⏎ on the `server` row opens the same sub-list an enum opens — never a text buffer — over the
// servers this session can move to, with "(current)" on the one it is ON (ADR 0037 decision 4). The
// list is the PROVIDER's answer at the moment the question is asked, so a `servers:` block that
// gained an entry mid-session offers it the next time the row is opened.
func TestSettingsServerRowPicksFromTheLiveList(t *testing.T) {
	servers := twoServers
	m := settingsServerModel(t, func() []ServerChoice { return servers }, &fakeSwitch{}, &settingsWriteLog{})

	opened := step(t, m, keyEnter())

	if opened.settings.kind != settingsEnumList {
		t.Fatalf("pane = %+v, want the value sub-list — the server row never opens a text buffer", opened.settings)
	}
	if opened.settings.sub != 0 {
		t.Errorf("sub = %d, want the row for the server this session is on", opened.settings.sub)
	}
	pane := strip(opened.renderSettings())
	for _, want := range []string{"server", "test-host", "remote", "(current)", settingsEnumHint} {
		if !strings.Contains(pane, want) {
			t.Errorf("the server sub-list does not show %q:\n%s", want, pane)
		}
	}

	// The block gains an entry under the closed pane; the next open offers it.
	servers = append(append([]ServerChoice{}, twoServers...), ServerChoice{Name: "laptop", Endpoint: "http://laptop:9"})
	reopened := step(t, step(t, opened, keyEsc()), keyEnter())
	if got := len(reopened.settingsVocabulary(settingsServerRow())); got != 3 {
		t.Fatalf("vocabulary = %d values, want the three the provider now answers with", got)
	}
	if pane := strip(reopened.renderSettings()); !strings.Contains(pane, "laptop") {
		t.Errorf("the reopened sub-list does not offer the entry added since:\n%s", pane)
	}
}

// Choosing a server MOVES the session: the switch seam is driven exactly as `/server` drives it, the
// fold states the move in the transcript, and the row shows the chosen name with the marker that says
// this session changed it. Nothing is written through the settings writer — the recorded choice IS
// this key's persistence (ADR 0036 decision 2), which the seam's own half performs.
func TestSettingsServerRowSwitchesTheSession(t *testing.T) {
	sw, log := &fakeSwitch{}, &settingsWriteLog{}
	m := settingsServerModel(t, staticServers(twoServers), sw, log)

	m = step(t, m, keyEnter()) // the sub-list, on test-host
	m = step(t, m, keyDown())  // remote
	m = step(t, m, keyEnter()) // commit

	if want := []string{"remote"}; !reflect.DeepEqual(sw.calls, want) {
		t.Fatalf("SwitchServer calls = %v, want %v", sw.calls, want)
	}
	if len(log.writes) != 0 || len(log.applies) != 0 {
		t.Errorf("writes = %+v, applies = %+v; the server row goes through neither", log.writes, log.applies)
	}
	if m.settings.kind != settingsKeyList {
		t.Errorf("pane = %+v, want the key list back — the question was answered", m.settings)
	}
	if got, want := m.settingsValueCell(settingsServerRow()), "remote"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q — the server now on the wire, marked as chosen here", got, want)
	}
	if got := m.settingsNote(settingsServerRow()); got != "" {
		t.Errorf("note = %q, want none: the move happened and the value cell says so", got)
	}
	if m.opts.Endpoint != twoServers[1].Endpoint {
		t.Errorf("endpoint = %q, want the moved-to server %q", m.opts.Endpoint, twoServers[1].Endpoint)
	}
	if got := plainTranscript(m); !strings.Contains(got, "switching server") {
		t.Errorf("the switch is not folded into the transcript:\n%s", got)
	}
}

// A refusal from the seam means the session did not move, and it lands on the ROW that asked rather
// than in a transcript this full-height pane is covering. Nothing is journaled: the row still shows
// the server the session is on, with no marker claiming a change.
func TestSettingsServerRowRefusalLandsOnTheRow(t *testing.T) {
	sw := &fakeSwitch{answer: func(string) (ServerSwitchResult, error) {
		return ServerSwitchResult{}, errors.New("dial tcp: refused")
	}}
	log := &settingsWriteLog{}
	m := settingsServerModel(t, staticServers(twoServers), sw, log)

	m = step(t, m, keyEnter())
	m = step(t, m, keyDown())
	m = step(t, m, keyEnter())

	row := settingsServerRow()
	if got, want := m.settingsNote(row), "✗ dial tcp: refused"; got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
	if got := m.settingsValueCell(row); got != row.Value {
		t.Errorf("value cell = %q, want the unchanged %q — a refused switch changed nothing", got, row.Value)
	}
	if m.opts.Endpoint != testOpts.Endpoint {
		t.Errorf("endpoint = %q, want the session left where it was (%q)", m.opts.Endpoint, testOpts.Endpoint)
	}
	if len(log.writes) != 0 {
		t.Errorf("writes = %+v, want none", log.writes)
	}
}

// Backspace is INERT on the `server` row, before and after this session has switched. The key's value
// is the recording of a switch (ADR 0036 decision 2) and the row is a second entrance to that seam and
// nothing else (ADR 0037 decision 5): a reset would splice the line away while the session went on
// running against the server it named — the file and the wire disagreeing, with nothing journaled. The
// legend says so too, naming no key that would do nothing here.
func TestSettingsServerRowTakesNoReset(t *testing.T) {
	sw, log := &fakeSwitch{}, &settingsWriteLog{}
	m := settingsServerModel(t, staticServers(twoServers), sw, log)

	if m.settingsResettable(settingsServerRow()) {
		t.Error("the server row offers a reset: ⌫ would erase the recorded choice the session is running on")
	}
	armed := step(t, m, keyBackspace())
	if armed.settings.kind != settingsKeyList {
		t.Fatalf("pane state = %d, want %d — backspace armed a reset on the server row",
			armed.settings.kind, settingsKeyList)
	}
	if pane := strip(armed.renderSettings()); !strings.Contains(pane, settingsNoResetHint) || strings.Contains(pane, "⌫ reset") {
		t.Errorf("the legend still advertises a reset this row does not take:\n%s", pane)
	}

	// And it stays inert once a switch HAS journaled the key — the journal is what arms ⌫ on every
	// other row (settingsResettable), so this is the case the kind has to answer.
	m = step(t, step(t, step(t, m, keyEnter()), keyDown()), keyEnter()) // switch to remote
	if _, edited := m.settingEditOf("server"); !edited {
		t.Fatalf("the switch journaled nothing; this case needs a journaled edit to be about anything")
	}
	m = step(t, step(t, m, keyBackspace()), keyEnter()) // ⌫ then the ⏎ that would confirm a reset

	if len(log.resets) != 0 {
		t.Errorf("resets = %+v, want none: the server row's line is the switch seam's to write", log.resets)
	}
	if len(log.writes) != 0 {
		t.Errorf("writes = %+v, want none", log.writes)
	}
	if got, want := m.settingsValueCell(settingsServerRow()), "remote"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q — the row still names the server the session is on", got, want)
	}
}

// Choosing the server the session is already on changes nothing, which is exactly why the row has to
// SAY so: the delegate answers in the transcript ([Model.switchToServer]) and this pane is drawn over
// it, so a ⏎ nobody could see the effect of would read as a keypress that did nothing. Nothing is
// switched and nothing is journaled — no marker claims a change that did not happen.
func TestSettingsServerRowSaysWhenItIsAlreadyOnTheChosenServer(t *testing.T) {
	sw, log := &fakeSwitch{}, &settingsWriteLog{}
	m := settingsServerModel(t, staticServers(twoServers), sw, log)

	m = step(t, step(t, m, keyEnter()), keyEnter()) // the sub-list opens on test-host; ⏎ confirms it

	row := settingsServerRow()
	if len(sw.calls) != 0 {
		t.Errorf("SwitchServer calls = %v, want none: the session is already there", sw.calls)
	}
	if len(m.settingEdits) != 0 {
		t.Errorf("edits = %+v, want none: nothing changed, so no marker may claim it did", m.settingEdits)
	}
	if got, want := m.settingsNote(row), "· "+settingsAlreadyOnNote+"test-host"; got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
	if got := m.settingsValueCell(row); got != row.Value {
		t.Errorf("value cell = %q, want the unchanged %q", got, row.Value)
	}
	if pane := strip(m.renderSettings()); !strings.Contains(pane, settingsAlreadyOnNote+"test-host") {
		t.Errorf("the confirmation is nowhere the human can read it with the pane open:\n%s", pane)
	}
	if got := plainTranscript(m); !strings.Contains(got, settingsAlreadyOnNote+"test-host") {
		t.Errorf("the transcript lost the answer /server gives:\n%s", got)
	}

	// It is the last act's outcome, not the row's condition: the next landed switch replaces it.
	m = step(t, step(t, step(t, m, keyEnter()), keyDown()), keyEnter())
	if got := m.settingsNote(row); got != "" {
		t.Errorf("note = %q, want none — the confirmation outlived the switch that answered it", got)
	}
}

// An edit APPLIES as well as persists (ADR 0037 decision 1), and every key that is not the renderer's
// own goes out through the binary's dispatcher: the pane hands it the same path and the same value it
// handed the writer, and knows nothing about the seam behind it. `mode` is the one key the pane also
// MIRRORS, because the footer renders the mode from opts.Mode — so the row shows the new value with no
// caveat, because there is nothing left to wait for.
func TestSettingsPaneModeEditAppliesLiveAndMarksNothing(t *testing.T) {
	rows := []SettingRow{{
		Path: "mode", Section: "Autonomy", Kind: SettingEnum, Value: "ask-before", Default: "ask-before",
		EnumValues: []string{"plan", "ask-before", "allow-edits", "auto"}, Editable: true,
		Desc: "Autonomy mode: how tool calls are gated.",
	}}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter()) // the sub-list, highlighted on ask-before
	m = step(t, m, keyDown())  // allow-edits
	m = step(t, m, keyEnter()) // commit

	if want := []settingEdit{{path: "mode", value: "allow-edits"}}; !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v", log.writes, want)
	}
	if want := []settingEdit{{path: "mode", value: "allow-edits"}}; !reflect.DeepEqual(log.applies, want) {
		t.Errorf("applies = %+v, want %+v — the persisted value goes straight to the dispatcher", log.applies, want)
	}
	if m.opts.Mode != domain.ModeAllowEdits {
		t.Errorf("opts.Mode = %q, want allow-edits (the footer renders the mode from it)", m.opts.Mode)
	}
	if got := m.settingsNote(rows[0]); got != "" {
		t.Errorf("marker = %q, want none: a live-applied edit has nothing to wait for", got)
	}
	if got, want := m.settingsValueCell(rows[0]), "allow-edits"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q — the value the session is now running", got, want)
	}
}

// settingsLiveBoolRow is one editable bool row that applies through the dispatcher — `bypass:` as the
// registry describes it, minus the restart gate no key keeps once its edit takes effect.
func settingsLiveBoolRow() SettingRow {
	return SettingRow{
		Path: "bypass", Section: "Mechanisms", Kind: SettingBool, Value: "false", Default: "false",
		Editable: true, Desc: "Run with Mechanisms off.",
	}
}

// A toggle persists and then applies, in that order and on the one keypress: the write seam is asked
// first, and only what the file accepted is handed to the dispatcher (ADR 0037 decision 1). A refused
// write reaches the apply seam not at all — the session must never run what the file does not say.
func TestSettingsPaneToggleAppliesWhatItPersisted(t *testing.T) {
	rows := []SettingRow{settingsLiveBoolRow()}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter())

	want := []settingEdit{{path: "bypass", value: "true"}}
	if !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v", log.writes, want)
	}
	if !reflect.DeepEqual(log.applies, want) {
		t.Errorf("applies = %+v, want %+v — what persisted is what applies", log.applies, want)
	}
	if got, want := m.settingsValueCell(rows[0]), "true"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q — the value the session is now running", got, want)
	}
	if got := m.settingsNote(rows[0]); got != "" {
		t.Errorf("marker = %q, want none: an applied edit has nothing to caveat", got)
	}

	// A refused write is not applied: the file is unchanged, so the session must be too.
	log.err = errors.New("config.yaml is read-only")
	m = step(t, m, keyEnter())
	if len(log.applies) != 1 {
		t.Errorf("applies = %+v, want only the first: a refused write has nothing to apply", log.applies)
	}
}

// A key that cannot land NOW lands at a boundary this session will cross, and the row says which
// (ADR 0037 decision 3). The note comes from the apply seam — the renderer holds no idea of what any
// key's boundary is — and it is the only deferral wording the surface has.
func TestSettingsPaneShowsTheApplyBoundaryNote(t *testing.T) {
	rows := []SettingRow{{
		Path: "context-files.enable", Section: "Prompt", Kind: SettingBool, Value: "false", Default: "true",
		Editable: true, Desc: "Fold the workspace context files into the system prompt.",
	}}
	log := &settingsWriteLog{applyNote: "applies at next clear"}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter())

	if got, want := m.settingsNote(rows[0]), "· applies at next clear"; got != want {
		t.Errorf("marker = %q, want %q", got, want)
	}
	if got, want := m.settingsValueCell(rows[0]), "true"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q — the value the file and the seam now agree on", got, want)
	}
	if pane := strip(m.renderSettings()); !strings.Contains(pane, "applies at next clear") {
		t.Errorf("the pane does not carry the boundary note:\n%s", pane)
	}
}

// An apply that fails AFTER a successful write does not unwind it (ADR 0037 decision 1): the file
// expresses what the human asked for, so the edit stands and the row reports the half that did not
// happen. The wording is its own — "saved — live apply failed" is a different sentence from a refused
// write, and has to read like one.
func TestSettingsPaneApplyFailureKeepsTheWriteAndSaysSo(t *testing.T) {
	rows := []SettingRow{settingsLiveBoolRow()}
	log := &settingsWriteLog{applyErr: errors.New("no server is bound yet")}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter())

	if want := []settingEdit{{path: "bypass", value: "true"}}; !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v — a failed apply must not unwind the write", log.writes, want)
	}
	if got, want := m.settingsValueCell(rows[0]), "true"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q: the file says it whatever the session runs", got, want)
	}
	want := "✗ saved — live apply failed: no server is bound yet"
	if got := m.settingsNote(rows[0]); got != want {
		t.Errorf("marker = %q, want %q", got, want)
	}
	if pane := strip(m.renderSettings()); !strings.Contains(pane, "saved — live apply failed") {
		t.Errorf("the pane does not report the failed apply:\n%s", pane)
	}

	// Re-committing retries the apply against the same persisted value, and a retry that lands
	// clears the failure — the row describes the last attempt, not a condition of the key.
	log.applyErr = nil
	m = step(t, m, keyEnter())
	if got := m.settingsNote(rows[0]); got != "" {
		t.Errorf("marker = %q, want none after a retry that landed", got)
	}
}

// The keys whose whole effect is this screen are applied by the renderer itself and never leave it:
// there is no engine on the other side of a spinner style, and routing one out to the binary and back
// would only be a longer way to reach the Model's own fields.
func TestSettingsPaneRendererOwnedKeysApplyWithoutTheSeam(t *testing.T) {
	tests := []struct {
		name  string
		row   SettingRow
		check func(t *testing.T, m Model)
	}{
		{
			name: "auto-title",
			row: SettingRow{
				Path: "auto-title", Section: "Session", Kind: SettingBool, Value: "true", Default: "true",
				Editable: true, Desc: "Name a new session from its first prompt.",
			},
			check: func(t *testing.T, m Model) {
				t.Helper()
				if m.opts.AutoTitle {
					t.Error("opts.AutoTitle is still on; the toggle did not reach the automatic naming call")
				}
			},
		},
		{
			name: "ui.show-scrollbar",
			row: SettingRow{
				Path: "ui.show-scrollbar", Section: "Interface", Kind: SettingBool, Value: "true",
				Default: "true", Editable: true, Desc: "Paint the transcript's scroll bar.",
			},
			check: func(t *testing.T, m Model) {
				t.Helper()
				if !m.opts.HideScrollbar {
					t.Error("opts.HideScrollbar is still false; the bar was not taken away")
				}
				// The bar's gutter is transcript width: the flip must lay the frame out again, or the
				// body would keep wrapping to a column it no longer pays for.
				if got, want := m.viewport.Width(), 80; got != want {
					t.Errorf("viewport width = %d, want %d — the hidden bar gives its column back", got, want)
				}
			},
		},
		{
			name: "ui.spinner-color",
			row: SettingRow{
				Path: "ui.spinner-color", Section: "Interface", Kind: SettingBool, Value: "true",
				Default: "true", Editable: true, Desc: "Run the spinner's colour loop.",
			},
			check: func(t *testing.T, m Model) {
				t.Helper()
				if m.spin.color {
					t.Error("the spinner is still running its colour loop")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rows := []SettingRow{tt.row}
			log := &settingsWriteLog{}
			m, _ := settingsEditModel(t, rows, log)

			m = step(t, m, keyEnter())

			if want := []settingEdit{{path: tt.row.Path, value: "false"}}; !reflect.DeepEqual(log.writes, want) {
				t.Fatalf("writes = %+v, want %+v", log.writes, want)
			}
			if len(log.applies) != 0 {
				t.Errorf("applies = %+v, want none: the renderer owns this key", log.applies)
			}
			tt.check(t, m)
		})
	}
}

// The cursor shape is the renderer's own too, and it is the one whose effect is not a field the pane
// reads back: it restates the textarea's cursor styles, which is what the terminal draws the caret
// from. A value this build's vocabulary does not know is reported rather than silently ignored.
func TestSettingsPaneCursorShapeAppliesAndRefusesTheUnknown(t *testing.T) {
	row := SettingRow{
		Path: "cursor-shape", Section: "Interface", Kind: SettingEnum, Value: "block", Default: "block",
		EnumValues: []string{"block", "underline", "bar"}, Editable: true,
		Desc: "The shape the prompt's caret is drawn with.",
	}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, []SettingRow{row}, log)

	m = step(t, m, keyEnter()) // the sub-list, on block
	m = step(t, m, keyDown())  // underline
	m = step(t, m, keyEnter()) // commit

	if m.opts.CursorShape != tea.CursorUnderline {
		t.Errorf("opts.CursorShape = %v, want underline", m.opts.CursorShape)
	}
	if got := m.input.Styles().Cursor.Shape; got != tea.CursorUnderline {
		t.Errorf("the input's cursor shape = %v, want underline — steadyCursor was not re-run", got)
	}
	if len(log.applies) != 0 {
		t.Errorf("applies = %+v, want none: the renderer owns this key", log.applies)
	}

	// A name the renderer has no shape for is an apply failure on the row, not a silent no-op: the
	// pane is not the only thing that can put a value in that file.
	unknown := SettingRow{Path: "cursor-shape", Kind: SettingString, Editable: true}
	m, _ = settingsEditModel(t, []SettingRow{unknown}, &settingsWriteLog{})
	m = step(t, typeSetting(t, step(t, m, keyEnter()), "hourglass"), keyEnter())

	if got := m.settingsNote(unknown); !strings.Contains(got, "saved — live apply failed") {
		t.Errorf("marker = %q, want the apply's own refusal", got)
	}
}

// An edit to an overridden row APPLIES like any other — a pane edit outranks an environment variable
// or a flag for the running session (ADR 0037 decision 4) — and what the row adds is the one thing that
// is not obvious from that: the override wins again at the next start. Without the note, an edit whose
// effect vanished on the next launch would look like an edit that had never landed.
func TestSettingsPaneOverriddenRowSaysTheOverrideOutranksItAtTheNextStart(t *testing.T) {
	rows := []SettingRow{{
		Path: "bypass", Section: "Mechanisms", Kind: SettingBool, Value: "true", Default: "false",
		Source: SettingFromEnv, SourceName: "APOGEE_BYPASS", Editable: true,
		Desc: "Run with Mechanisms off.",
	}}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter())

	edits := []settingEdit{{path: "bypass", value: "false"}}
	if !reflect.DeepEqual(log.writes, edits) {
		t.Fatalf("writes = %+v, want %+v", log.writes, edits)
	}
	if !reflect.DeepEqual(log.applies, edits) {
		t.Errorf("applies = %+v, want %+v — the edit outranks the override for THIS session", log.applies, edits)
	}
	if got, want := m.settingsValueCell(rows[0]), "false"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q — the value the session is running after the edit", got, want)
	}
	want := "· APOGEE_BYPASS outranks at next launch"
	if got := m.settingsNote(rows[0]); got != want {
		t.Errorf("marker = %q, want %q", got, want)
	}
	if pane := strip(m.renderSettings()); !strings.Contains(pane, "(env)") || !strings.Contains(pane, want) {
		t.Errorf("the pane does not carry both the override marker and the note:\n%s", pane)
	}
}

// Nothing this pane paints defers to a restart any more (ADR 0037 decision 8). Every edit idiom the
// surface has — a renderer-owned toggle, a buffered string, a masked write, an overridden key, a key
// with a boundary note, and a reset — leaves the same thing behind: the value the session is running
// with a ` *` beside it. The old "(next launch)" markers described a state that cannot happen, so no
// row, note or frame may still produce one.
func TestSettingsPaneNeverDefersToTheNextLaunch(t *testing.T) {
	rows := []SettingRow{
		settingsBoolRow(),   // renderer-owned: applied without the seam
		settingsStringRow(), // the buffered string
		{Path: "api-key", Section: "Upstream", Kind: SettingString, Value: "••••",
			Editable: true, Masked: true, Desc: "Bearer token."},
		{Path: "bypass", Section: "Mechanisms", Kind: SettingBool, Value: "true", Default: "false",
			Source: SettingFromEnv, SourceName: "APOGEE_BYPASS", Editable: true, Desc: "Mechanisms off."},
		{Path: "context-files.enable", Section: "System prompt", Kind: SettingBool, Value: "false",
			Default: "true", Editable: true, Desc: "Fold the workspace context files in."},
	}
	log := &settingsWriteLog{applyNote: "applies at next clear"}
	m, _ := settingsEditModel(t, rows, log)

	for i := range rows {
		m.settings.selected = i
		m = step(t, m, keyEnter()) // a bool toggles on the spot; a string opens its buffer
		if m.settings.kind == settingsValueBuffer {
			m = step(t, typeSetting(t, m, "x"), keyEnter())
		}
	}
	m.settings.selected = 0
	m = step(t, step(t, m, keyBackspace()), keyEnter()) // and the reset idiom, over the first edit

	if len(m.settingEdits) != len(rows) {
		t.Fatalf("edits = %+v, want one per row: an idiom above did not land", m.settingEdits)
	}
	if pane := strip(m.renderSettings()); strings.Contains(pane, "(next launch)") {
		t.Errorf("a row still defers to the next launch:\n%s", pane)
	}
	for _, row := range rows {
		if got := m.settingsNote(row); strings.Contains(got, "(next launch)") {
			t.Errorf("row %q note = %q, want no deferral marker", row.Path, got)
		}
		if got := m.settingsValueCell(row); !strings.HasSuffix(got, settingsEditMarker) {
			t.Errorf("row %q value cell = %q, want the session-edit marker on an edited row", row.Path, got)
		}
	}
}

// Only a relaunch clears the marker (ADR 0037 decision 8) — dismissing the pane is not one. The
// journal lives on the Model rather than on the overlay for exactly this: the provider still answers
// with the value THIS RUN resolved (`true`), so a reopened pane that had forgotten the edit would tell
// a session running `false` that it was running `true` — the one lie the marker exists to prevent.
func TestSettingsPaneEditMarkerSurvivesAReopen(t *testing.T) {
	rows := []SettingRow{settingsBoolRow()}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter()) // toggles `auto-title` to false, persists and applies it
	want := "false" + settingsEditMarker
	if got := m.settingsValueCell(rows[0]); got != want {
		t.Fatalf("value cell = %q, want %q before the pane is dismissed", got, want)
	}

	m = openSettingsPane(t, step(t, m, keyEsc()))

	if len(m.settingEdits) != 1 {
		t.Fatalf("edits = %+v, want the one this session made: closing the pane dropped its journal", m.settingEdits)
	}
	if got := m.settingsValueCell(rows[0]); got != want {
		t.Errorf("value cell = %q, want %q — the reopened pane forgot what this session changed", got, want)
	}
	if pane := strip(m.renderSettings()); !strings.Contains(pane, want) {
		t.Errorf("the reopened pane does not show the edited value and its marker:\n%s", pane)
	}
	if !m.settingsResettable(rows[0]) {
		t.Error("the reopened pane will not reset a key it wrote: the journal behind ⌫ died with the overlay")
	}
}

// A refused write changes NOTHING but the row's own line: no edit recorded, no value moved, and the
// reason on the row — where the human can read it, which the transcript behind a full-height pane is
// not. A write that lands afterwards clears it.
func TestSettingsPaneWriteErrorStaysOnTheRowAndChangesNothing(t *testing.T) {
	rows := []SettingRow{settingsBoolRow()}
	log := &settingsWriteLog{err: errors.New("config.yaml is read-only")}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter())

	if len(log.writes) != 0 || len(m.settingEdits) != 0 {
		t.Fatalf("a refused write left writes %+v / edits %+v, want neither", log.writes, m.settingEdits)
	}
	if got := m.settingsValueCell(rows[0]); got != "true" {
		t.Errorf("value cell = %q, want the row untouched", got)
	}
	if got := m.settingsNote(rows[0]); !strings.Contains(got, "config.yaml is read-only") {
		t.Errorf("marker = %q, want the refusal's reason", got)
	}
	if pane := strip(m.renderSettings()); !strings.Contains(pane, "config.yaml is read-only") {
		t.Errorf("the pane does not show the refusal:\n%s", pane)
	}
	if !m.settings.open {
		t.Error("a refused write closed the pane")
	}

	log.err = nil
	m = step(t, m, keyEnter())

	if got, want := m.settingsValueCell(rows[0]), "false"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q — a landed write replaces the refusal", got, want)
	}
	if got := m.settingsNote(rows[0]); got != "" {
		t.Errorf("marker = %q, want the refusal gone", got)
	}
}

// The nil-seam degrade, on the row: a build (or a Driver, ADR 0031) that composed Options without the
// write seam has an honest sentence to say and nothing to write.
func TestSettingsPaneWithoutAWriterSaysSoOnTheRow(t *testing.T) {
	rows := []SettingRow{settingsBoolRow()}
	opts := testOpts
	opts.SettingsRows = func() []SettingRow { return rows }
	m := openSettingsPane(t, newTestModelEng(t, &fakeEngine{}, opts))

	m = step(t, m, keyEnter())

	if len(m.settingEdits) != 0 {
		t.Fatalf("edits = %+v, want none: nothing was written", m.settingEdits)
	}
	if got := m.settingsNote(rows[0]); !strings.Contains(got, noSettingsWriterNote) {
		t.Errorf("marker = %q, want %q", got, noSettingsWriterNote)
	}
}

// ⏎ never writes a row the registry does not let this surface write, and opens no step on one either: a
// structured block and the confinement keys /confine keeps single-homed (ADR 0012). The pointer cell
// already says where each one IS edited. Backspace is refused on the same rows for the same reason —
// nothing here may remove their lines.
func TestSettingsPaneEnterNeverWritesARowItMayNotEdit(t *testing.T) {
	rows := []SettingRow{
		{Path: "servers", Section: "Upstream", Kind: SettingStructured, Value: "3 servers",
			EditPointer: "⏎ opens $EDITOR", ExternalEdit: true, Desc: "The named server list."},
		{Path: "unconfined-hosts", Section: "Confinement", Kind: SettingStructured, Value: "2 hosts",
			EditPointer: "use /confine", Desc: "Machines acknowledged as disposable."},
	}
	log := &settingsWriteLog{}
	m, eng := settingsEditModel(t, rows, log)

	for i := range rows {
		for _, key := range []tea.KeyPressMsg{keyEnter(), keyBackspace()} {
			m.settings.selected = i
			m = step(t, m, key)
			if len(log.writes) != 0 || len(log.resets) != 0 {
				t.Fatalf("%v on %s persisted %+v / %+v", key, rows[i].Path, log.writes, log.resets)
			}
			if m.settings.kind != settingsKeyList {
				t.Fatalf("%v on %s opened a step on a row it may not edit", key, rows[i].Path)
			}
		}
	}
	if got := eng.modesSet(); len(got) != 0 {
		t.Errorf("engine SetMode = %v, want none", got)
	}
}

// The sub-list is a view of the SELECTED row, and the rows are re-derived under it — so a key that
// went away takes its question with it: the pane falls back to its list rather than committing a value
// to whatever now sits at that index.
func TestSettingsEnumSubListFallsBackWhenItsKeyGoesAway(t *testing.T) {
	rows := []SettingRow{settingsBoolRow(), settingsEnumRow()}
	log := &settingsWriteLog{}
	opts := testOpts
	opts.SettingsRows = func() []SettingRow { return rows }
	opts.WriteSetting = log.write
	m := openSettingsPane(t, newTestModelEng(t, &fakeEngine{}, opts))

	m = step(t, m, keyDown())  // the enum row
	m = step(t, m, keyEnter()) // its value sub-list
	if m.settings.kind != settingsEnumList {
		t.Fatalf("pane = %+v, want the sub-list open", m.settings)
	}

	rows = []SettingRow{settingsBoolRow()} // the provider now answers with the bool row alone
	m = step(t, m, keyEnter())

	if m.settings.kind != settingsKeyList {
		t.Errorf("pane = %+v, want the key list back", m.settings)
	}
	if len(log.writes) != 0 {
		t.Errorf("the orphaned sub-list persisted %+v, want nothing", log.writes)
	}
	if pane := strip(m.renderSettings()); !strings.Contains(pane, settingsHint) {
		t.Errorf("the pane is not showing its key list:\n%s", pane)
	}
}

// ----------------------------------------------------------------------------
// Editing: the string/int buffer, validation and reset-to-default (ADR 0035)
// ----------------------------------------------------------------------------

// settingsStringRow is one editable string row. Its path is a fixture, not a live registry key —
// the pane's string editing is key-agnostic, so the row stands for whichever string keys the
// registry carries (`server:` today).
func settingsStringRow() SettingRow {
	return SettingRow{
		Path: "endpoint", Section: "Upstream", Kind: SettingString, Value: "http://box:1111",
		Editable: true, Desc: "The OpenAI-compatible LLM server URL.",
	}
}

// settingsIntRow is one editable int row — `present.port:`, whose default is its "pick one for me".
func settingsIntRow() SettingRow {
	return SettingRow{
		Path: "present.port", Section: "Present", Kind: SettingInt, Value: "0", Default: "0",
		Editable: true, Desc: "The document server's port; 0 picks a free one.",
	}
}

// ⏎ on a string or an int row opens a FIELD on the row (lineeditor.go), seeded with the value the key
// holds and with the caret at the end of it — so a human correcting a port edits it rather than
// retyping it — and ⏎ commits what the field holds through the write seam. The field's text and its
// caret are painted in the value's own column, and the legend switches to the two keys that end the
// edit.
func TestSettingsPaneBufferEditsAStringAndPersistsIt(t *testing.T) {
	rows := []SettingRow{settingsStringRow()}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter())

	if m.settings.kind != settingsValueBuffer {
		t.Fatalf("pane = %+v, want the edit buffer open", m.settings)
	}
	if m.settings.editor.value() != "http://box:1111" {
		t.Errorf("buffer = %q, want it seeded with the value the key holds", m.settings.editor.value())
	}
	pane := strip(m.renderSettings())
	for _, want := range []string{"endpoint", "http://box:1111" + settingsCaret, settingsBufferHint} {
		if !strings.Contains(pane, want) {
			t.Errorf("the buffered row does not show %q:\n%s", want, pane)
		}
	}

	// Backspace deletes inside an edit — at the caret, which is at the end here; it does not arm a
	// reset there.
	for range len("1111") {
		m = step(t, m, keyBackspace())
	}
	m = typeSetting(t, m, "2222")
	if m.settings.kind != settingsValueBuffer {
		t.Fatalf("pane = %+v, want the buffer still open (backspace edits inside it)", m.settings)
	}

	m = step(t, m, keyEnter())

	if want := []settingEdit{{path: "endpoint", value: "http://box:2222"}}; !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v", log.writes, want)
	}
	if m.settings.kind != settingsKeyList || m.settings.editor.value() != "" {
		t.Errorf("pane = %+v after the commit, want the buffer closed and empty", m.settings)
	}
	if got, want := m.settingsValueCell(rows[0]), "http://box:2222"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q", got, want)
	}
	// A re-opened buffer starts from what was WRITTEN, not from the resolution the pane opened over.
	if reopened := step(t, m, keyEnter()); reopened.settings.editor.value() != "http://box:2222" {
		t.Errorf("re-opened buffer = %q, want the value this pane persisted", reopened.settings.editor.value())
	}
}

// A name list is edited as the one LINE it is written on: `context-files.names:` reaches the pane as
// a string row (cmd/apogee's settingKind), so the field opens seeded with the list the row shows, and
// the line the human hands back goes to BOTH seams verbatim — the binary parses it once, into the
// file's spelling and into the engine's list, and this renderer parses nothing. The boundary note the
// apply answers with lands on the row, because a name list moves at the next session boundary.
func TestSettingsPaneEditsANameListAsOneLine(t *testing.T) {
	rows := []SettingRow{{
		Path: "context-files.names", Section: "System prompt", Kind: SettingString,
		Value: "[AGENTS.md]", Default: "[AGENTS.md]", Editable: true,
		Desc: "Workspace-root file names folded into the system prompt.",
	}}
	log := &settingsWriteLog{applyNote: "applies at next clear"}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter())

	if m.settings.editor.value() != "[AGENTS.md]" {
		t.Fatalf("buffer = %q, want it seeded with the list the row shows", m.settings.editor.value())
	}
	for range len("]") {
		m = step(t, m, keyBackspace())
	}
	m = typeSetting(t, m, ", docs/CLAUDE.md]")
	m = step(t, m, keyEnter())

	edit := settingEdit{path: "context-files.names", value: "[AGENTS.md, docs/CLAUDE.md]"}
	if want := []settingEdit{edit}; !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v", log.writes, want)
	}
	if want := []settingEdit{edit}; !reflect.DeepEqual(log.applies, want) {
		t.Fatalf("applies = %+v, want the same line the write took: %+v", log.applies, want)
	}
	if got, want := m.settingsValueCell(rows[0]), edit.value+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q", got, want)
	}
	if got, want := m.settingsNote(rows[0]), "· applies at next clear"; got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}

// The caret keys the value field answers — the widget's own key map, which is the chat box's
// (lineeditor.go). alt+← is the word jump on every terminal; the Kitty-only ctrl+← is not needed to
// state that the jump is there.
func keyLeft() tea.KeyPressMsg     { return tea.KeyPressMsg{Code: tea.KeyLeft} }
func keyRight() tea.KeyPressMsg    { return tea.KeyPressMsg{Code: tea.KeyRight} }
func keyHome() tea.KeyPressMsg     { return tea.KeyPressMsg{Code: tea.KeyHome} }
func keyEnd() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyEnd} }
func keyDelete() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyDelete} }
func keyWordLeft() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt} }

// The two keys that insert a newline in the CHAT box (newPromptEditor rebinds them there because
// plain ⏎ submits). In a settings value they must do nothing at all.
func keyAltEnter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt} }
func keyCtrlJ() tea.KeyPressMsg    { return tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl} }

// pressSettings drives a run of keypresses through the open pane in order, the way a human presses
// them — a caret walk and the rune it ends in are one gesture, and reading them as one line is what
// makes the case below legible.
func pressSettings(t *testing.T, m Model, keys ...tea.KeyPressMsg) Model {
	t.Helper()
	for _, k := range keys {
		m = step(t, m, k)
	}
	return m
}

// The value row is edited in a real field, not an append-and-pop buffer: the caret MOVES and every
// edit lands where it stands (plan item 10, spec requirement 7). Each case opens the field on a row
// seeded with its own value, walks the caret and types, then asserts both halves — what the field
// holds, and what the row PAINTS, since the caret glyph is drawn at the caret and is therefore the
// only thing on the screen that says where the next keystroke goes.
func TestSettingsPaneValueFieldEditsAtTheCaret(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		seed  string
		keys  []tea.KeyPressMsg
		value string // what the field holds afterwards
		cell  string // what the row's value column paints
	}{{
		name:  "a rune inserts where ← left the caret",
		seed:  "http://box:1234",
		keys:  []tea.KeyPressMsg{keyLeft(), keyLeft(), keyLeft(), keyLeft(), keyRune('9')},
		value: "http://box:91234",
		cell:  "http://box:9" + settingsCaret + "1234",
	}, {
		name:  "→ walks back over what ← passed",
		seed:  "http://box:1234",
		keys:  []tea.KeyPressMsg{keyLeft(), keyLeft(), keyRight(), keyRune('0')},
		value: "http://box:12304",
		cell:  "http://box:1230" + settingsCaret + "4",
	}, {
		name:  "home seats the caret at the front",
		seed:  "http://box:1234",
		keys:  []tea.KeyPressMsg{keyHome(), keyRune('X')},
		value: "Xhttp://box:1234",
		cell:  "X" + settingsCaret + "http://box:1234",
	}, {
		name:  "end brings it back to the tail",
		seed:  "http://box:1234",
		keys:  []tea.KeyPressMsg{keyHome(), keyEnd(), keyRune('5')},
		value: "http://box:12345",
		cell:  "http://box:12345" + settingsCaret,
	}, {
		name:  "backspace takes the rune BEFORE the caret, not the last one",
		seed:  "http://box:1234",
		keys:  []tea.KeyPressMsg{keyLeft(), keyLeft(), keyBackspace()},
		value: "http://box:134",
		cell:  "http://box:1" + settingsCaret + "34",
	}, {
		name:  "delete takes the rune the caret stands on",
		seed:  "http://box:1234",
		keys:  []tea.KeyPressMsg{keyHome(), keyDelete()},
		value: "ttp://box:1234",
		cell:  settingsCaret + "ttp://box:1234",
	}, {
		name:  "the word jump crosses to the start of the last word",
		seed:  "open -a Safari",
		keys:  []tea.KeyPressMsg{keyWordLeft(), keyRune('X')},
		value: "open -a XSafari",
		cell:  "open -a X" + settingsCaret + "Safari",
	}, {
		// The field is single-line (lineEditor.singleLine), and it has to be: a value carrying a
		// newline would break the row it is painted in — the popup module lays out one line per row.
		name:  "no key can split a single-line value",
		seed:  "http://box:1234",
		keys:  []tea.KeyPressMsg{keyAltEnter(), keyCtrlJ()},
		value: "http://box:1234",
		cell:  "http://box:1234" + settingsCaret,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := settingsStringRow()
			row.Value = tc.seed
			m, _ := settingsEditModel(t, []SettingRow{row}, &settingsWriteLog{})

			m = step(t, m, keyEnter()) // the field opens seeded, caret at the end of the value
			m = pressSettings(t, m, tc.keys...)

			if got := m.settings.editor.value(); got != tc.value {
				t.Errorf("field = %q, want %q", got, tc.value)
			}
			if pane := strip(m.renderSettings()); !strings.Contains(pane, tc.cell) {
				t.Errorf("the row does not paint %q:\n%s", tc.cell, pane)
			}
		})
	}
}

// An edit made in the middle of a value commits and cancels exactly as one typed at its end: ⏎
// persists what the field HOLDS (where the caret stands is not part of a value), esc drops the field
// whole and writes nothing. The field goes with either ending — a pane back in its key list is not
// holding a half-typed value.
func TestSettingsPaneValueFieldCommitsAndCancelsAMidStringEdit(t *testing.T) {
	rows := []SettingRow{settingsStringRow()} // http://box:1111
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	m = pressSettings(t, m, keyEnter(), keyLeft(), keyLeft(), keyLeft(), keyLeft(), keyRune('9'))
	m = step(t, m, keyEnter())

	want := []settingEdit{{path: "endpoint", value: "http://box:91111"}}
	if !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v", log.writes, want)
	}
	if !reflect.DeepEqual(log.applies, want) {
		t.Errorf("applies = %+v, want %+v — a mid-string edit applies like any other", log.applies, want)
	}
	if m.settings.kind != settingsKeyList || m.settings.editor.value() != "" {
		t.Errorf("pane = %+v after the commit, want the field closed and empty", m.settings)
	}

	// And the cancel: a caret walk and a typed rune, then esc — the file keeps what the commit above
	// put in it and the row goes on showing that.
	cancelled := pressSettings(t, m, keyEnter(), keyHome(), keyRune('X'), keyEsc())

	if !reflect.DeepEqual(log.writes, want) {
		t.Errorf("writes = %+v, want %+v — esc persists nothing", log.writes, want)
	}
	if cancelled.settings.kind != settingsKeyList || cancelled.settings.editor.value() != "" {
		t.Errorf("pane = %+v after esc, want the field closed and empty", cancelled.settings)
	}
	if got, cell := cancelled.settingsValueCell(rows[0]), "http://box:91111"+settingsEditMarker; got != cell {
		t.Errorf("value cell = %q, want %q — the abandoned edit left no trace", got, cell)
	}
}

// esc abandons the edit: the buffer closes, nothing is written, and the value the run resolved is back
// in its column. An empty buffer commits nothing either — ⏎ on a cleared field is an abandoned edit far
// more often than a request to persist emptiness, and taking a value away is what the reset below is.
func TestSettingsPaneBufferCancelAndEmptyCommitWriteNothing(t *testing.T) {
	rows := []SettingRow{settingsIntRow()}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	cancelled := step(t, typeSetting(t, step(t, m, keyEnter()), "8080"), keyEsc())

	if cancelled.settings.kind != settingsKeyList || cancelled.settings.editor.value() != "" {
		t.Errorf("pane = %+v after esc, want the buffer closed and empty", cancelled.settings)
	}
	if !cancelled.settings.open {
		t.Error("esc out of the buffer closed the whole pane; it backs out of the EDIT")
	}
	if len(log.writes) != 0 {
		t.Fatalf("an abandoned edit persisted %+v", log.writes)
	}
	if got := cancelled.settingsValueCell(rows[0]); got != "0" {
		t.Errorf("value cell = %q, want the row untouched", got)
	}

	emptied := step(t, m, keyEnter())
	for range len("0") {
		emptied = step(t, emptied, keyBackspace())
	}
	emptied = step(t, emptied, keyEnter())

	if len(log.writes) != 0 {
		t.Fatalf("an empty buffer persisted %+v, want nothing", log.writes)
	}
	if emptied.settings.kind != settingsKeyList {
		t.Errorf("pane = %+v, want the buffer closed", emptied.settings)
	}
}

// A value the binary refuses keeps its buffer OPEN with the reason on the row: what a key may hold is
// the registry's business and the write seam is what asks (ADR 0011's thin renderer), so the pane's
// half of a validation failure is to leave the human's own text in front of them to correct. A value
// that lands afterwards closes the buffer and clears the refusal.
func TestSettingsPaneBufferKeepsARefusedValueForCorrection(t *testing.T) {
	rows := []SettingRow{settingsIntRow()}
	log := &settingsWriteLog{err: errors.New("apogee: invalid present.port \"99999\": want a TCP port in 0-65535")}
	m, _ := settingsEditModel(t, rows, log)

	m = typeSetting(t, step(t, m, keyEnter()), "99999")
	m = step(t, m, keyEnter())

	if m.settings.kind != settingsValueBuffer {
		t.Fatalf("pane = %+v, want the buffer still open on a refused value", m.settings)
	}
	if m.settings.editor.value() != "099999" {
		t.Errorf("buffer = %q, want the typed value kept for correction", m.settings.editor.value())
	}
	if len(m.settingEdits) != 0 {
		t.Errorf("edits = %+v, want none: a refused write changed no file", m.settingEdits)
	}
	if got := m.settingsNote(rows[0]); !strings.Contains(got, "0-65535") {
		t.Errorf("marker = %q, want the refusal's reason", got)
	}
	// On the row itself the reason is truncated to the column, so what is asserted there is its FRONT:
	// a refusal is worded key-first for exactly this reason (cmd/apogee/registry.go).
	if pane := strip(m.renderSettings()); !strings.Contains(pane, "invalid present.port") ||
		!strings.Contains(pane, settingsCaret) {
		t.Errorf("the pane shows neither the refusal nor the still-open buffer:\n%s", pane)
	}

	log.err = nil
	m = step(t, m, keyBackspace()) // "09999"
	m = step(t, m, keyEnter())

	if want := []settingEdit{{path: "present.port", value: "09999"}}; !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v", log.writes, want)
	}
	if m.settings.kind != settingsKeyList {
		t.Errorf("pane = %+v, want the buffer closed once the value landed", m.settings)
	}
	if got := m.settingsNote(rows[0]); strings.Contains(got, "0-65535") {
		t.Errorf("marker = %q, want the refusal gone", got)
	}
}

// An abandoned edit takes its refusal with it: the ✗ described THAT buffer, and a row nobody is editing
// must not go on reporting a failure the human walked away from.
func TestSettingsPaneBufferCancelClearsTheRefusal(t *testing.T) {
	rows := []SettingRow{settingsIntRow()}
	log := &settingsWriteLog{err: errors.New("not a port")}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter())
	m = step(t, m, keyEnter()) // commit the seeded value; refused
	if got := m.settingsNote(rows[0]); !strings.Contains(got, "not a port") {
		t.Fatalf("marker = %q, want the refusal — test premise broken", got)
	}

	m = step(t, m, keyEsc())

	if got := m.settingsNote(rows[0]); got != "" {
		t.Errorf("marker = %q after esc, want nothing left of the abandoned edit", got)
	}
}

// The masked key is the one row the buffer does not seed: the pane holds a mask and not the secret
// ([SettingRow]), so an api-key is typed whole. What is typed IS visible while typing — a human cannot
// check a token they cannot see — and what the row shows afterwards is the MASK with the session-edit
// marker beside it: the ` *` says the key was changed here, and nothing on the row repeats the secret.
func TestSettingsPaneMaskedRowBuffersVisiblyAndKeepsItsMask(t *testing.T) {
	rows := []SettingRow{{
		Path: "api-key", Section: "Upstream", Kind: SettingString, Value: "••••",
		Editable: true, Masked: true, Desc: "Bearer token sent on every request.",
	}}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter())
	if m.settings.editor.value() != "" {
		t.Fatalf("buffer = %q, want it empty: the pane never held the old secret", m.settings.editor.value())
	}
	m = typeSetting(t, m, "sk-live-42")
	if pane := strip(m.renderSettings()); !strings.Contains(pane, "sk-live-42"+settingsCaret) {
		t.Errorf("the buffer does not show what is being typed:\n%s", pane)
	}

	m = step(t, m, keyEnter())

	if want := []settingEdit{{path: "api-key", value: "sk-live-42"}}; !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v", log.writes, want)
	}
	if got, want := m.settingsValueCell(rows[0]), "••••"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q — the row never repeats a secret", got, want)
	}
	if got := m.settingsNote(rows[0]); got != "" {
		t.Errorf("marker = %q, want none: the marked mask is the whole of what the row says", got)
	}
	if pane := strip(m.renderSettings()); strings.Contains(pane, "sk-live-42") {
		t.Errorf("the committed secret is still on the screen:\n%s", pane)
	}
}

// Reset is two keypresses on purpose: backspace ARMS it and the hint line asks, ⏎ confirms, esc cancels.
// What lands is ResetSetting — the key's line removed — and the row then reports the default it went back
// to, on the same terms a write reports its value.
func TestSettingsPaneResetArmsConfirmsAndCancels(t *testing.T) {
	rows := []SettingRow{{
		Path: "auto-title", Section: "Session", Kind: SettingBool, Value: "false", Default: "true",
		Editable: true, Desc: "Name a new session from its first prompt.",
	}}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	armed := step(t, m, keyBackspace())

	if armed.settings.kind != settingsResetArmed {
		t.Fatalf("pane = %+v, want the reset armed", armed.settings)
	}
	if pane := strip(armed.renderSettings()); !strings.Contains(pane, settingsResetHint) {
		t.Errorf("the hint line does not ask for the confirmation:\n%s", pane)
	}
	if len(log.resets) != 0 {
		t.Fatalf("arming reset %+v; arming is not the act", log.resets)
	}

	// esc cancels the question and leaves the key alone.
	cancelled := step(t, armed, keyEsc())
	if cancelled.settings.kind != settingsKeyList || !cancelled.settings.open {
		t.Errorf("pane = %+v after esc, want the key list back", cancelled.settings)
	}
	if len(log.resets) != 0 {
		t.Fatalf("a cancelled reset called %+v", log.resets)
	}

	confirmed := step(t, armed, keyEnter())

	if want := []string{"auto-title"}; !reflect.DeepEqual(log.resets, want) {
		t.Fatalf("resets = %+v, want %+v", log.resets, want)
	}
	if len(log.writes) != 0 {
		t.Errorf("the reset WROTE %+v; a reset removes the line, it does not write the default", log.writes)
	}
	if confirmed.settings.kind != settingsKeyList {
		t.Errorf("pane = %+v, want the key list back after the confirmation", confirmed.settings)
	}
	if got, want := confirmed.settingsValueCell(rows[0]), "true"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q — the default the key went back to, marked as changed here", got, want)
	}
}

// A reset of a key that defaults to NOTHING says so in words: a marker that trailed off into a blank is
// the one thing a row must not do after a deliberate act. The masked key is that case too — a removed
// line left no secret to keep quiet about, so it reads like every other emptied key.
func TestSettingsPaneResetOfAnUnsetDefaultSaysUnset(t *testing.T) {
	for _, row := range []SettingRow{
		settingsStringRow(),
		{Path: "api-key", Section: "Upstream", Kind: SettingString, Value: "••••",
			Editable: true, Masked: true, Desc: "Bearer token."},
	} {
		t.Run(row.Path, func(t *testing.T) {
			log := &settingsWriteLog{}
			m, _ := settingsEditModel(t, []SettingRow{row}, log)

			m = step(t, step(t, m, keyBackspace()), keyEnter())

			if want := []string{row.Path}; !reflect.DeepEqual(log.resets, want) {
				t.Fatalf("resets = %+v, want %+v", log.resets, want)
			}
			if got, want := m.settingsValueCell(row), settingsUnsetValue+settingsEditMarker; got != want {
				t.Errorf("value cell = %q, want %q", got, want)
			}
		})
	}
}

// A row already showing its default has no line to remove, so backspace arms nothing at all: a
// confirmation prompt for a no-op is worse than a keypress that does nothing. Nor is a reset offered on
// a row this surface may not write.
func TestSettingsPaneResetIsANoOpOnADefaultValuedRow(t *testing.T) {
	rows := []SettingRow{
		settingsIntRow(), // value "0" == default "0"
		{Path: "servers", Section: "Upstream", Kind: SettingStructured, Value: "3 servers",
			EditPointer: "⏎ opens $EDITOR", ExternalEdit: true, Desc: "The named server list."},
	}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	for i := range rows {
		m.settings.selected = i
		m = step(t, m, keyBackspace())
		if m.settings.kind != settingsKeyList {
			t.Fatalf("backspace on %s armed a reset with nothing to reset", rows[i].Path)
		}
		m = step(t, m, keyEnter()) // and the ⏎ that would have confirmed it resets nothing
		if len(log.resets) != 0 {
			t.Fatalf("reset %+v on %s, want none", log.resets, rows[i].Path)
		}
	}

	// It becomes resettable the moment this pane writes it — the file then carries a line to remove.
	m.settings.selected = 0
	m = step(t, typeSetting(t, step(t, m, keyEnter()), "8080"), keyEnter())
	m = step(t, step(t, m, keyBackspace()), keyEnter())

	if want := []string{"present.port"}; !reflect.DeepEqual(log.resets, want) {
		t.Errorf("resets = %+v, want %+v", log.resets, want)
	}
}

// A RESET applies exactly as a write does, through the same dispatcher: the session drops back to the
// ladder's default now, and the row shows it with no caveat.
func TestSettingsPaneResetOfModeAppliesTheDefaultLive(t *testing.T) {
	rows := []SettingRow{{
		Path: "mode", Section: "Autonomy", Kind: SettingEnum, Value: "auto", Default: "ask-before",
		EnumValues: []string{"plan", "ask-before", "allow-edits", "auto"}, Editable: true,
		Desc: "Autonomy mode: how tool calls are gated.",
	}}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, step(t, m, keyBackspace()), keyEnter())

	if want := []string{"mode"}; !reflect.DeepEqual(log.resets, want) {
		t.Fatalf("resets = %+v, want %+v", log.resets, want)
	}
	if want := []settingEdit{{path: "mode", value: "ask-before"}}; !reflect.DeepEqual(log.applies, want) {
		t.Errorf("applies = %+v, want %+v — a reset applies the default it went back to", log.applies, want)
	}
	if m.opts.Mode != domain.ModeAskBefore {
		t.Errorf("opts.Mode = %q, want ask-before (the footer renders the mode from it)", m.opts.Mode)
	}
	if got := m.settingsNote(rows[0]); got != "" {
		t.Errorf("marker = %q, want none: a live-applied reset has nothing to wait for", got)
	}
	if got, want := m.settingsValueCell(rows[0]), "ask-before"+settingsEditMarker; got != want {
		t.Errorf("value cell = %q, want %q — the default the session is now running", got, want)
	}
}

// The two new steps are views of the SELECTED row and the rows are re-derived under them, so a key that
// went away takes its buffer (or its armed reset) with it rather than letting a ⏎ land on whatever now
// sits at that index — the enum sub-list's contract, on the same predicate.
func TestSettingsSecondStepsFallBackWhenTheirKeyGoesAway(t *testing.T) {
	for _, tc := range []struct {
		name string
		open tea.KeyPressMsg
		kind settingsKind
	}{
		{"the edit buffer", keyEnter(), settingsValueBuffer},
		{"an armed reset", keyBackspace(), settingsResetArmed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := []SettingRow{settingsBoolRow(), settingsStringRow()}
			log := &settingsWriteLog{}
			opts := testOpts
			opts.SettingsRows = func() []SettingRow { return rows }
			opts.WriteSetting = log.write
			opts.ResetSetting = log.reset
			m := openSettingsPane(t, newTestModelEng(t, &fakeEngine{}, opts))

			m = step(t, m, keyDown()) // the string row
			m = step(t, m, tc.open)
			if m.settings.kind != tc.kind {
				t.Fatalf("pane = %+v, want %v open", m.settings, tc.kind)
			}

			rows = []SettingRow{settingsBoolRow()} // the provider drops the row under the step
			m = step(t, m, keyEnter())

			if m.settings.kind != settingsKeyList || m.settings.editor.value() != "" {
				t.Errorf("pane = %+v, want the key list back and no buffer", m.settings)
			}
			if len(log.writes) != 0 || len(log.resets) != 0 {
				t.Errorf("the orphaned step persisted %+v / %+v, want nothing", log.writes, log.resets)
			}
			if pane := strip(m.renderSettings()); !strings.Contains(pane, settingsHint) {
				t.Errorf("the pane is not showing its key list:\n%s", pane)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The multi-line text editor (plan item 14, ADR 0037 decision 10)
// ----------------------------------------------------------------------------

// keyCtrlS is the key that COMMITS the multi-line field — ⏎ belongs to the value there.
func keyCtrlS() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl} }

// settingsTextRow is the one text row the schema has — `system-prompt-text:` as the registry
// describes it (cmd/apogee/registry.go): a value cell holding a SUMMARY of the prose, and the prose
// itself beside it for the editor to open on.
func settingsTextRow() SettingRow {
	return SettingRow{
		Path: "system-prompt-text", Section: "System prompt", Kind: SettingText,
		Value: "2 lines", Text: "You are apogee.\nWork step by step.\n", Editable: true,
		Desc: "The system prompt written inline — the standing instructions sent ahead of your first message.",
	}
}

// ⏎ on a text row opens a multi-line field over the whole list, seeded with the prose the key holds;
// ⏎ inside it inserts a newline; ctrl+s persists and applies what was written, and the row goes back
// to a summary of it with the session-edit marker.
func TestSettingsPaneTextEditorWritesTheProseOnCtrlS(t *testing.T) {
	rows := []SettingRow{settingsTextRow()}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	m = step(t, m, keyEnter())

	if m.settings.kind != settingsTextEditor {
		t.Fatalf("pane = %+v, want the multi-line field open", m.settings)
	}
	if got := m.settings.editor.value(); got != "You are apogee.\nWork step by step." {
		t.Errorf("field = %q, want it seeded with the prompt, its trailing newline trimmed", got)
	}
	pane := strip(m.renderSettings())
	for _, want := range []string{"Description:", "You are apogee.", "Work step by step.", settingsTextHint} {
		if !strings.Contains(pane, want) {
			t.Errorf("the open field does not show %q:\n%s", want, pane)
		}
	}
	if strings.Contains(pane, settingsHint) {
		t.Errorf("the field is showing the key list's legend:\n%s", pane)
	}

	// ⏎ is the VALUE's key here: it adds a line rather than committing.
	m = pressSettings(t, m, keyEnter(), keyRune('A'), keyRune('n'), keyRune('d'))
	if m.settings.kind != settingsTextEditor {
		t.Fatalf("pane = %+v, want the field still open — ⏎ inserts a newline", m.settings)
	}
	if len(log.writes) != 0 {
		t.Fatalf("⏎ persisted %+v, want nothing", log.writes)
	}

	m = step(t, m, keyCtrlS())

	want := []settingEdit{{path: "system-prompt-text", value: "You are apogee.\nWork step by step.\nAnd"}}
	if !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v", log.writes, want)
	}
	if !reflect.DeepEqual(log.applies, want) {
		t.Errorf("applies = %+v, want %+v — a prompt applies on the keypress that persists it", log.applies, want)
	}
	if m.settings.kind != settingsKeyList || m.settings.editor.value() != "" {
		t.Errorf("pane = %+v after the commit, want the field closed and empty", m.settings)
	}
	if got, cell := m.settingsValueCell(rows[0]), "3 lines"+settingsEditMarker; got != cell {
		t.Errorf("value cell = %q, want %q — a row shows a summary of prose, never the prose", got, cell)
	}
	// A re-opened field starts from what was WRITTEN, not from the resolution the pane opened over.
	if reopened := step(t, m, keyEnter()); reopened.settings.editor.value() != want[0].value {
		t.Errorf("re-opened field = %q, want the prompt this pane persisted", reopened.settings.editor.value())
	}
}

// esc discards the whole edit — the key that walks away from a page of prose must not be the one that
// persists it — and a field cleared to nothing writes nothing either: taking the prompt away is the
// reset backspace arms, which is what the binary's own validator says in as many words.
func TestSettingsPaneTextEditorDiscardsOnEsc(t *testing.T) {
	rows := []SettingRow{settingsTextRow()}
	log := &settingsWriteLog{}
	m, _ := settingsEditModel(t, rows, log)

	discarded := pressSettings(t, step(t, m, keyEnter()), keyRune('X'), keyEsc())

	if discarded.settings.kind != settingsKeyList || discarded.settings.editor.value() != "" {
		t.Errorf("pane = %+v after esc, want the field closed and empty", discarded.settings)
	}
	if !discarded.settings.open {
		t.Error("esc out of the field closed the whole pane; it backs out of the EDIT")
	}
	if len(log.writes) != 0 || len(log.applies) != 0 {
		t.Fatalf("an abandoned edit persisted %+v / applied %+v", log.writes, log.applies)
	}
	if got := discarded.settingsValueCell(rows[0]); got != "2 lines" {
		t.Errorf("value cell = %q, want the row untouched", got)
	}

	emptied := step(t, m, keyEnter())
	for range len(settingsTextRow().Text) {
		emptied = step(t, emptied, keyBackspace())
	}
	emptied = step(t, emptied, keyCtrlS())

	if len(log.writes) != 0 {
		t.Fatalf("an empty field persisted %+v, want nothing", log.writes)
	}
	if emptied.settings.kind != settingsKeyList {
		t.Errorf("pane = %+v, want the field closed", emptied.settings)
	}
}

// A prompt the binary refuses keeps the field OPEN with the reason on the row it came from — the edit
// buffer's contract, for its reason: the human fixes the placeholder they mistyped rather than writing
// the prompt again.
func TestSettingsPaneTextEditorKeepsARefusedPrompt(t *testing.T) {
	rows := []SettingRow{settingsTextRow()}
	log := &settingsWriteLog{err: errors.New(`apogee: invalid system-prompt-text: prompt: unknown placeholder "{{bogus}}"`)}
	m, _ := settingsEditModel(t, rows, log)

	m = pressSettings(t, step(t, m, keyEnter()), keyRune('!'), keyCtrlS())

	if m.settings.kind != settingsTextEditor {
		t.Fatalf("pane = %+v, want the field still open on a refused prompt", m.settings)
	}
	if got := m.settings.editor.value(); !strings.HasSuffix(got, "!") {
		t.Errorf("field = %q, want the typed prose kept for correction", got)
	}
	if len(m.settingEdits) != 0 {
		t.Errorf("edits = %+v, want none: a refused write changed no file", m.settingEdits)
	}
	if got := m.settingsNote(rows[0]); !strings.Contains(got, "unknown placeholder") {
		t.Errorf("note = %q, want the refusal's reason", got)
	}
}

// The field is a FIELD: the caret moves through the prose and the glyph says where the next keystroke
// lands, on the line it stands on rather than always on the last one.
func TestSettingsPaneTextEditorPaintsTheCaretWhereItStands(t *testing.T) {
	rows := []SettingRow{settingsTextRow()}
	m, _ := settingsEditModel(t, rows, &settingsWriteLog{})

	m = pressSettings(t, step(t, m, keyEnter()), keyHome(), keyRune('>'))

	if got := m.settings.editor.value(); got != "You are apogee.\n>Work step by step." {
		t.Errorf("field = %q, want the rune inserted at the second line's start", got)
	}
	if pane := strip(m.renderSettings()); !strings.Contains(pane, ">"+settingsCaret+"Work step by step.") {
		t.Errorf("the caret is not drawn where it stands:\n%s", pane)
	}
}

// ----------------------------------------------------------------------------
// Pasting into a settings field (spec requirement 7 — a field the human can paste into)
// ----------------------------------------------------------------------------

// A bracketed paste lands in whichever field the pane has open, and NEVER in the chat box behind it:
// the pane is full-height, so a paste that fell through would fill a draft the human cannot see. The
// value buffer flattens what it takes — a value carrying a newline would break the one row it is
// painted in — while the multi-line field keeps the lines, which is what it is for.
func TestSettingsPasteLandsInTheOpenField(t *testing.T) {
	cases := []struct {
		name    string
		row     SettingRow
		content string
		want    string // the field's value after the paste
	}{
		{"value buffer", settingsStringRow(), "/one", "http://box:1111/one"},
		{"value buffer flattens a pasted newline", settingsStringRow(), "/one\n", "http://box:1111/one "},
		{"multi-line field keeps the lines", settingsTextRow(), "\nAnd stop.",
			"You are apogee.\nWork step by step.\nAnd stop."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := settingsEditModel(t, []SettingRow{c.row}, &settingsWriteLog{})
			m = step(t, m, keyEnter()) // the field opens seeded with the value, caret at its end

			m = step(t, m, tea.PasteMsg{Content: c.content})

			if got := m.settings.editor.value(); got != c.want {
				t.Errorf("field = %q, want %q", got, c.want)
			}
			if got := m.input.Value(); got != "" {
				t.Errorf("the chat box behind the pane took the paste: %q", got)
			}
		})
	}
}

// With no field open the pane still SWALLOWS a paste, exactly as it swallows every key it does not act
// on: the box it would otherwise land in is one the human cannot read past a full-height pane.
func TestSettingsPasteIsSwallowedWithNoFieldOpen(t *testing.T) {
	m, _ := settingsEditModel(t, []SettingRow{settingsStringRow()}, &settingsWriteLog{})

	m = step(t, m, tea.PasteMsg{Content: "stray"})

	if got := m.input.Value(); got != "" {
		t.Errorf("the chat box behind the pane took the paste: %q", got)
	}
	if m.settings.kind != settingsKeyList {
		t.Errorf("pane = %+v, want the key list untouched", m.settings)
	}
}

// A paste is an EDIT, so it drops the field's drag-selection for handleKey's own reason: the value is
// about to change under a span whose offsets would then name other runes.
func TestSettingsPasteDropsTheFieldSelection(t *testing.T) {
	m, _ := settingsEditModel(t, []SettingRow{settingsStringRow()}, &settingsWriteLog{})
	m = step(t, m, keyEnter())
	m.settings.sel = promptSel{active: true, anchorOff: 0, headOff: 4}

	if pasted := step(t, m, tea.PasteMsg{Content: "x"}); pasted.settings.sel.active {
		t.Errorf("the paste left the drag-selection armed: %+v", pasted.settings.sel)
	}
}

// ctrl+v in a settings field asks the widget for the clipboard — the binding is live in BOTH fields
// now that the reply has a route home — and the reply, whose type is the widget package's own and
// unexported, is delivered by the route rather than by its type (settingsEditorMsg, the arm
// [Model.Update] ends on).
func TestSettingsFieldTakesCtrlVAndItsReplyIsRoutable(t *testing.T) {
	for _, c := range []struct {
		name string
		row  SettingRow
	}{{"value buffer", settingsStringRow()}, {"multi-line field", settingsTextRow()}} {
		t.Run(c.name, func(t *testing.T) {
			m, _ := settingsEditModel(t, []SettingRow{c.row}, &settingsWriteLog{})
			m = step(t, m, keyEnter())

			_, cmd := stepCmd(t, m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
			if cmd == nil {
				t.Error("ctrl+v in the field returned no Cmd; the widget's clipboard read is switched off")
			}
			// The reply comes back as an opaque Msg: what makes it reach this field is that the pane
			// claims it on the way past, which is the whole of the route.
			if _, _, claimed := m.settingsEditorMsg(tea.PasteMsg{Content: "x"}); !claimed {
				t.Error("the pane does not claim a Msg for its own open field")
			}
		})
	}
	closed := settingsModel(t, []SettingRow{settingsStringRow()})
	if _, _, claimed := closed.settingsEditorMsg(tea.PasteMsg{Content: "x"}); claimed {
		t.Error("a closed pane claimed a Msg; the chat box's own messages are the chat box's")
	}
}

// ----------------------------------------------------------------------------
// The $EDITOR round trip (ADR 0037 decision 5)
// ----------------------------------------------------------------------------

// settingsStructuredRow is one row holding a shape no field can express — `servers:` as the registry
// describes it: read-only in the pane, and edited in the human's own editor on the key's own line.
func settingsStructuredRow() SettingRow {
	return SettingRow{
		Path: "servers", Section: "Upstream", Kind: SettingStructured, Value: "3 servers",
		EditPointer: pointerForTest, ExternalEdit: true, Desc: "The named server list.",
	}
}

// pointerForTest is the wording the binary projects onto those rows; the pane only paints it.
const pointerForTest = "⏎ opens $EDITOR"

// externalEditLog is the two seams of the round trip as spies: which key was asked for a command
// line, and what the re-read reported back.
type externalEditLog struct {
	asked     []string
	argv      []string
	specErr   error
	applied   []AppliedSetting
	reloadErr error
	reloads   int
}

func (l *externalEditLog) spec(path string) ([]string, error) {
	l.asked = append(l.asked, path)
	if l.specErr != nil {
		return nil, l.specErr
	}
	return l.argv, nil
}

func (l *externalEditLog) reload() ([]AppliedSetting, error) {
	l.reloads++
	if l.reloadErr != nil {
		return nil, l.reloadErr
	}
	return l.applied, nil
}

// externalEditModel is settingsEditModel with the round trip's two seams wired as well.
func externalEditModel(t *testing.T, rows []SettingRow, log *settingsWriteLog, edit *externalEditLog) Model {
	t.Helper()
	opts := testOpts
	opts.SettingsRows = func() []SettingRow { return rows }
	opts.WriteSetting = log.write
	opts.ResetSetting = log.reset
	opts.ApplySetting = log.apply
	opts.ExternalEditSpec = edit.spec
	opts.ReloadConfig = edit.reload
	return openSettingsPane(t, newTestModelEng(t, &fakeEngine{}, opts))
}

// ⏎ on a structured row asks the binary for a command line and suspends into it. Nothing is written
// and nothing is journaled yet — the file is the human's to change, and the journal is the return
// trip's business.
func TestSettingsPaneStructuredRowOpensTheExternalEditor(t *testing.T) {
	rows := []SettingRow{settingsStructuredRow()}
	log, edit := &settingsWriteLog{}, &externalEditLog{argv: []string{"vi", "+7", "/tmp/config.yaml"}}
	m := externalEditModel(t, rows, log, edit)

	next, cmd := stepCmd(t, m, keyEnter())
	m = next

	if want := []string{"servers"}; !reflect.DeepEqual(edit.asked, want) {
		t.Errorf("spec asked for %v, want %v", edit.asked, want)
	}
	if cmd == nil {
		t.Fatal("⏎ on a structured row returned no Cmd; the editor was never launched")
	}
	if len(log.writes) != 0 || len(m.settingEdits) != 0 {
		t.Errorf("the launch wrote %+v and journaled %+v; both belong to the return trip", log.writes, m.settingEdits)
	}
	if got := m.settingsNote(rows[0]); got != "· "+pointerForTest {
		t.Errorf("note = %q, want the row's own pointer — nothing failed", got)
	}
}

// The offer stands only between runs (ADR 0037 binding C): suspending the whole program into an
// editor mid-stream would take the answer off the screen and leave the applies queued behind it. The
// launcher actuation is the in-flight state a human can actually press ⏎ during — a streaming Step
// leaves this pane's keys unrouted altogether (the overlay is idle-only).
func TestSettingsPaneRefusesTheExternalEditMidRun(t *testing.T) {
	rows := []SettingRow{settingsStructuredRow()}
	edit := &externalEditLog{argv: []string{"vi", "/tmp/config.yaml"}}
	m := externalEditModel(t, rows, &settingsWriteLog{}, edit)
	m.actuation.inFlight = true

	next, cmd := stepCmd(t, m, keyEnter())
	m = next

	if cmd != nil || len(edit.asked) != 0 {
		t.Errorf("a mid-run ⏎ launched an editor (cmd=%v, asked=%v)", cmd != nil, edit.asked)
	}
	if got := m.settingsNote(rows[0]); got != "✗ "+settingsEditBusyNote {
		t.Errorf("note = %q, want the wait note %q", got, settingsEditBusyNote)
	}
}

// A build with no spec seam says so on the row, the nil-seam degrade every other act in this pane
// takes — and launches nothing.
func TestSettingsPaneSaysWhenThereIsNoExternalEditor(t *testing.T) {
	rows := []SettingRow{settingsStructuredRow()}
	opts := testOpts
	opts.SettingsRows = func() []SettingRow { return rows }
	m := openSettingsPane(t, newTestModelEng(t, &fakeEngine{}, opts))

	next, cmd := stepCmd(t, m, keyEnter())
	m = next

	if cmd != nil {
		t.Error("⏎ launched something with no spec seam wired")
	}
	if got := m.settingsNote(rows[0]); got != "✗ "+noExternalEditNote {
		t.Errorf("note = %q, want %q", got, noExternalEditNote)
	}
}

// The return trip: every key the re-read found changed is journaled — so its row shows the new value
// with the ` *` that says this session changed it — and applied through the same seam an in-pane
// commit applies through, boundary note and all.
func TestSettingsPaneAppliesWhatTheEditorChanged(t *testing.T) {
	rows := []SettingRow{
		settingsStructuredRow(),
		{Path: "context-files.names", Section: "System prompt", Kind: SettingString,
			Value: "[AGENTS.md]", Editable: true, Desc: "Workspace files folded into the prompt."},
	}
	log := &settingsWriteLog{applyNote: "applies at next clear"}
	edit := &externalEditLog{applied: []AppliedSetting{
		{Path: "servers", Value: "4 servers"},
		{Path: "context-files.names", Value: "[AGENTS.md, CLAUDE.md]"},
	}}
	m := externalEditModel(t, rows, log, edit)

	m = step(t, m, settingsEditedMsg{path: "servers"})

	if edit.reloads != 1 {
		t.Fatalf("reloads = %d, want exactly one re-read per round trip", edit.reloads)
	}
	if want := []settingEdit{
		{path: "servers", value: "4 servers"},
		{path: "context-files.names", value: "[AGENTS.md, CLAUDE.md]"},
	}; !reflect.DeepEqual(log.applies, want) {
		t.Fatalf("applies = %+v, want %+v", log.applies, want)
	}
	if len(log.writes) != 0 {
		t.Errorf("the round trip wrote %+v; the human's own editor already changed the file", log.writes)
	}
	if got, want := m.settingsValueCell(rows[0]), "4 servers"+settingsEditMarker; got != want {
		t.Errorf("servers cell = %q, want %q", got, want)
	}
	if got := m.settingsNote(rows[1]); got != "· applies at next clear" {
		t.Errorf("note = %q, want the boundary note the apply handed back", got)
	}
}

// A renderer-owned key changed in the file reaches its live home too: those keys ARE this Model's
// own fields, so nothing behind the dispatcher would have anything to do with them.
func TestSettingsPaneAppliesItsOwnKeysFromTheEditor(t *testing.T) {
	rows := []SettingRow{settingsStructuredRow(), settingsBoolRow()}
	log := &settingsWriteLog{}
	edit := &externalEditLog{applied: []AppliedSetting{{Path: "auto-title", Value: "false"}}}
	m := externalEditModel(t, rows, log, edit)

	m = step(t, m, settingsEditedMsg{path: "servers"})

	if m.opts.AutoTitle {
		t.Error("auto-title stayed on; a renderer-owned key must apply on the return trip")
	}
	if len(log.applies) != 0 {
		t.Errorf("a renderer-owned key was routed out to the dispatcher: %+v", log.applies)
	}
	if got, want := m.settingsValueCell(rows[1]), "false"+settingsEditMarker; got != want {
		t.Errorf("auto-title cell = %q, want %q", got, want)
	}
}

// A reload that could not parse or validate the file applies nothing and says why, on the row the
// human launched from — which is where they go back in from.
func TestSettingsPaneReportsAReloadItCouldNotMake(t *testing.T) {
	rows := []SettingRow{settingsStructuredRow()}
	edit := &externalEditLog{reloadErr: errors.New("apogee: parse config: line 4")}
	m := externalEditModel(t, rows, &settingsWriteLog{}, edit)

	m = step(t, m, settingsEditedMsg{path: "servers"})

	if len(m.settingEdits) != 0 {
		t.Errorf("a refused reload journaled %+v; nothing landed", m.settingEdits)
	}
	if got := m.settingsNote(rows[0]); !strings.Contains(got, "parse config: line 4") {
		t.Errorf("note = %q, want the reload's own reason", got)
	}
}

// An editor that could not run — or that exited non-zero, which is how an editor SAYS to discard
// (`:cq`) — ends the round trip without a re-read.
func TestSettingsPaneDoesNotReReadAfterAFailedEditor(t *testing.T) {
	rows := []SettingRow{settingsStructuredRow()}
	edit := &externalEditLog{applied: []AppliedSetting{{Path: "servers", Value: "9 servers"}}}
	m := externalEditModel(t, rows, &settingsWriteLog{}, edit)

	m = step(t, m, settingsEditedMsg{path: "servers", err: errors.New("exit status 1")})

	if edit.reloads != 0 {
		t.Errorf("reloads = %d, want none after an editor that did not finish cleanly", edit.reloads)
	}
	if got := m.settingsNote(rows[0]); !strings.Contains(got, "exit status 1") {
		t.Errorf("note = %q, want the editor's own failure", got)
	}
}

// ----------------------------------------------------------------------------
// The colour-scheme row: a dynamic vocabulary and a live apply (ADR 0039)
// ----------------------------------------------------------------------------

// settingsSchemeRow is the `ui.color-scheme:` row as the registry describes it
// (cmd/apogee/registry.go): an enum to the pane, because picking a scheme is picking a value from a
// list — but with EnumValues deliberately empty, because what may be picked is whatever the schemes
// folder holds right now and no static table can name it (settingsVocabulary).
func settingsSchemeRow() SettingRow {
	return SettingRow{
		Path: "ui.color-scheme", Section: "Interface", Kind: SettingEnum, Value: "dark", Default: "dark",
		Editable: true,
		Desc:     "Palette the screen is drawn in; ~/.apogee/schemes/<name>.yaml shadows a built-in.",
	}
}

// stubScheme is a palette whose every role carries value, so a theme built from it is recognisable
// by sampling any one of them — the fixture a live switch is proved BY (a scheme that shared a tone
// with the default could not tell an applied switch from an ignored one).
func stubScheme(value string) scheme.Scheme {
	s := scheme.Default()
	s.Error, s.Surface, s.UserText = value, value, value
	return s
}

// settingsSchemeModel is the pane OPEN over that one row with both colour-scheme seams wired: the
// list the picker offers and the resolve behind an answer to it. The three settings seams are wired
// too, so a test can watch the write and the apply on the same keypress.
func settingsSchemeModel(t *testing.T, log *settingsWriteLog, list []string,
	resolve func(string) (scheme.Scheme, []string)) Model {
	t.Helper()
	rows := []SettingRow{settingsSchemeRow()}
	opts := testOpts
	opts.SettingsRows = func() []SettingRow { return rows }
	opts.WriteSetting, opts.ResetSetting, opts.ApplySetting = log.write, log.reset, log.apply
	opts.ListSchemes = func() []string { return list }
	opts.ResolveScheme = resolve
	return openSettingsPane(t, newTestModelEng(t, &fakeEngine{}, opts))
}

// ⏎ on the colour-scheme row opens the same sub-list an enum opens, over the schemes the SESSION
// discovered rather than over a vocabulary the row carried: the built-ins plus every file in the
// human's schemes folder, which is a list that changes while the program runs and therefore cannot
// come from the registry (ADR 0039 design call 6). The scheme in force wears "(current)".
func TestSettingsPaneOffersTheSchemesTheSessionDiscovers(t *testing.T) {
	m := settingsSchemeModel(t, &settingsWriteLog{}, []string{"dark", "light", "mine"},
		func(string) (scheme.Scheme, []string) { return scheme.Default(), nil })

	opened := step(t, m, keyEnter())

	if opened.settings.kind != settingsEnumList {
		t.Fatalf("pane = %+v, want the value sub-list open", opened.settings)
	}
	pane := strip(opened.renderSettings())
	for _, want := range []string{"ui.color-scheme", "dark", "light", "mine", "(current)"} {
		if !strings.Contains(pane, want) {
			t.Errorf("the sub-list does not show %q:\n%s", want, pane)
		}
	}
	// The sub-list opens ON the scheme the key holds, which is what makes ⏎⏎ a confirmation.
	if opened.settings.sub != 0 {
		t.Errorf("sub = %d, want 0 — the highlight opens on the current scheme", opened.settings.sub)
	}
}

// An unwired [Options.ListSchemes] leaves the row with nothing to offer, and a sub-list over an
// empty vocabulary is a pane asking a question with no answers: ⏎ opens nothing at all, the same
// degrade a `servers:` block that names nothing takes.
func TestSettingsPaneSchemeRowOpensNothingWithoutADiscoverySeam(t *testing.T) {
	rows := []SettingRow{settingsSchemeRow()}
	opts := testOpts
	opts.SettingsRows = func() []SettingRow { return rows }
	m := openSettingsPane(t, newTestModelEng(t, &fakeEngine{}, opts))

	if opened := step(t, m, keyEnter()); opened.settings.kind != settingsKeyList {
		t.Errorf("pane = %+v, want the key list still — an empty vocabulary opens no question", opened.settings)
	}
}

// The live apply itself, and the whole of what ADR 0039's picker promises: the chosen scheme is
// persisted, RESOLVED again from the seam (so an edited file lands without a restart), and every
// style is rebuilt from what came back — with the memoised block paints thrown away, because each of
// them is in the palette that just stopped being the one on screen (paintcache.go), and a
// tea.ClearScreen asked for, because the terminal still holds the old one outside the frame.
func TestSettingsPaneAppliesAColorSchemeLive(t *testing.T) {
	log := &settingsWriteLog{}
	var asked []string
	m := settingsSchemeModel(t, log, []string{"dark", "light"}, func(name string) (scheme.Scheme, []string) {
		asked = append(asked, name)
		return stubScheme("#123456"), nil
	})
	if m.transcript.paints == nil {
		t.Fatal("the test model has no paint cache; the invalidation below would prove nothing")
	}
	m.transcript.paints.store(0, paintKey{width: 40}, blockPaint{})

	// Open the sub-list, walk to the second scheme, commit it.
	switched, cmd := stepCmd(t, step(t, step(t, m, keyEnter()), keyDown()), keyEnter())

	if want := []settingEdit{{path: "ui.color-scheme", value: "light"}}; !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v", log.writes, want)
	}
	if !reflect.DeepEqual(asked, []string{"light"}) {
		t.Fatalf("the resolver was asked for %v, want exactly the chosen scheme", asked)
	}
	if got := hexOf(switched.th.errorFg); got != "#123456" {
		t.Errorf("the model's error tone = %s; want the switched scheme's #123456 — the theme did not move", got)
	}
	if got := hexOf(switched.th.errorText.GetForeground()); got != "#123456" {
		t.Errorf("errorText fg = %s; want #123456 — the STYLES were not rebuilt", got)
	}
	if n := len(switched.transcript.paints.rows); n != 0 {
		t.Errorf("the paint cache still holds %d block(s) painted in the previous palette", n)
	}
	if got, want := cmdMsg(cmd), tea.ClearScreen(); !reflect.DeepEqual(got, want) {
		t.Errorf("the switch produced %#v, want tea.ClearScreen's Msg %#v", got, want)
	}
	// A renderer-owned key never leaves the renderer, colour scheme included.
	if len(log.applies) != 0 {
		t.Errorf("applies = %+v, want none — the scheme is applied inside the pane", log.applies)
	}
	// And the Options carry the scheme now in force, so a report can name it.
	if switched.opts.ColorSchemeName != "light" {
		t.Errorf("ColorSchemeName = %q, want %q", switched.opts.ColorSchemeName, "light")
	}
}

// The forgiving load has a voice (ADR 0039 design call 11): a switch that resolved with complaints
// still lands — the palette that comes back is always usable — and each complaint becomes one
// EPHEMERAL transcript note, while the row the human is looking at says how many there were. The
// pane is drawn over that transcript, so without the row's own sentence they would answer the
// picker and see nothing at all.
func TestSettingsPaneNotesWhatALiveSchemeSwitchWarnedAbout(t *testing.T) {
	const first = `color-scheme "mine.yaml": key "error": bad hex "#zz0000" — using default`
	const second = `color-scheme "mine.yaml": unknown key "backdrop" — ignored`
	rows := []SettingRow{settingsSchemeRow()}
	m := settingsSchemeModel(t, &settingsWriteLog{}, []string{"dark", "mine"},
		func(string) (scheme.Scheme, []string) { return stubScheme("#654321"), []string{first, second} })

	switched := step(t, step(t, step(t, m, keyEnter()), keyDown()), keyEnter())

	for _, want := range []string{first, second} {
		if !hasEntry(switched, entryNote, want) {
			t.Errorf("no note carries %q; entries = %+v", want, switched.transcript.entries)
		}
	}
	for _, e := range switched.transcript.entries {
		if e.kind == entryNote && e.text == first && !e.ephemeral {
			t.Error("the switch's warning is persisted; it is re-derived at every resolve")
		}
	}
	if got := switched.settingsNote(rows[0]); !strings.Contains(got, "applied with 2 warnings") {
		t.Errorf("row note = %q, want it to count the warnings the switch collected", got)
	}
	// The switch still LANDED: a warning is not a refusal.
	if got := hexOf(switched.th.errorFg); got != "#654321" {
		t.Errorf("the model's error tone = %s; want the switched scheme's #654321", got)
	}
}

// Without a resolver there is nothing to switch WITH, and the honest sentence is the one the row
// gives every apply that could not happen: the key is in the file, so the next start is drawn in it.
// The write is not unwound (ADR 0037 decision 1).
func TestSettingsPaneSaysASchemeSwitchNeedsAResolver(t *testing.T) {
	rows := []SettingRow{settingsSchemeRow()}
	log := &settingsWriteLog{}
	opts := testOpts
	opts.SettingsRows = func() []SettingRow { return rows }
	opts.WriteSetting, opts.ResetSetting, opts.ApplySetting = log.write, log.reset, log.apply
	opts.ListSchemes = func() []string { return []string{"dark", "light"} }
	m := openSettingsPane(t, newTestModelEng(t, &fakeEngine{}, opts))

	switched := step(t, step(t, step(t, m, keyEnter()), keyDown()), keyEnter())

	if want := []settingEdit{{path: "ui.color-scheme", value: "light"}}; !reflect.DeepEqual(log.writes, want) {
		t.Fatalf("writes = %+v, want %+v — a failed apply does not unwind the write", log.writes, want)
	}
	if got := switched.settingsNote(rows[0]); !strings.Contains(got, settingsApplyFailedNote) {
		t.Errorf("note = %q, want the saved-but-not-applied sentence", got)
	}
}
