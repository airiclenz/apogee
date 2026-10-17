package tui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The frame chords — ctrl+c and ctrl+l pass through every modal pane (Model.frameChord)
// ----------------------------------------------------------------------------

// frameChordPane is one modal claimant the frame chords must pass through: how to open it over a
// fresh model, and the pane's own state, read before and after a chord to show the chord left it
// alone.
type frameChordPane struct {
	name  string
	open  func(*testing.T) Model
	state func(Model) any
}

// frameChordPanes is every modal claimant of keyClaimOrder in every state it can stand in: the
// /sessions browser, the picker and the /workflows view, and the /settings pane on its key list and
// in each second step it opens (settingsSubStepCases).
func frameChordPanes() []frameChordPane {
	panes := []frameChordPane{
		{
			name: "sessions browser",
			open: func(t *testing.T) Model {
				m := newTestModel(t)
				m.sessionBrowser = sessionBrowser{open: true}
				return m
			},
			state: func(m Model) any { return m.sessionBrowser },
		},
		{
			name: "picker",
			open: func(t *testing.T) Model {
				m := newTestModel(t)
				m.picker = picker{open: true, kind: pickerModel}
				return m
			},
			state: func(m Model) any { return m.picker },
		},
		{
			name: "workflows view",
			open: func(t *testing.T) Model {
				m := newTestModel(t)
				m.workflowsPane = workflowsPane{open: true}
				return m
			},
			state: func(m Model) any { return m.workflowsPane },
		},
	}
	keyList := settingsSubStepCase{"key list", settingsStringRow(), "endp", nil, settingsKeyList}
	for _, c := range append([]settingsSubStepCase{keyList}, settingsSubStepCases()...) {
		panes = append(panes, frameChordPane{
			name:  "settings " + c.name,
			open:  func(t *testing.T) Model { return openSettingsStep(t, c) },
			state: func(m Model) any { return m.settings },
		})
	}
	return panes
}

// openSettingsStep opens the /settings pane over c's row, filters it to c.filter and opens c's step,
// failing the test when the pane is not then in c's kind.
func openSettingsStep(t *testing.T, c settingsSubStepCase) Model {
	t.Helper()
	m, _ := settingsEditModel(t, []SettingRow{settingsIntRow(), c.row}, &settingsWriteLog{})
	m = typeSetting(t, m, c.filter)
	m = pressSettings(t, m, c.open...)
	if m.settings.kind != c.kind {
		t.Fatalf("pane = %+v, want the %s open", m.settings, c.name)
	}
	return m
}

// settingsSubStepOf is the case of settingsSubStepCases that opens kind.
func settingsSubStepOf(t *testing.T, kind settingsKind) settingsSubStepCase {
	t.Helper()
	for _, c := range settingsSubStepCases() {
		if c.kind == kind {
			return c
		}
	}
	t.Fatalf("no settings sub-step case opens kind %v", kind)
	return settingsSubStepCase{}
}

// quitRequested reports whether the Model and Cmd a keypress left are a quit: the program exiting
// on the spot (tea.Quit), or a quit deferred behind a worker or a closing flush (quitting). The Cmd
// is run only when it can be the side-effect-free tea.Quit.
func quitRequested(m Model, cmd tea.Cmd) bool {
	if m.quitting {
		return true
	}
	_, isQuit := cmdMsg(cmd).(tea.QuitMsg)
	return isQuit
}

// A first ctrl+c under any modal pane arms the quit double-tap and leaves the pane exactly as it
// stood; a second one quits.
func TestFrameChordCtrlCQuitsUnderEveryModalPane(t *testing.T) {
	t.Parallel()
	for _, pane := range frameChordPanes() {
		t.Run(pane.name, func(t *testing.T) {
			t.Parallel()
			m := pane.open(t)
			before := pane.state(m)

			armed := step(t, m, keyCtrlC())

			if armed.lastCtrlC.IsZero() {
				t.Errorf("ctrl+c under the %s did not arm the quit", pane.name)
			}
			if after := pane.state(armed); !reflect.DeepEqual(after, before) {
				t.Errorf("ctrl+c changed the %s:\n got %+v\nwant %+v", pane.name, after, before)
			}
			if quit, cmd := ctrlCQuit(t, m); !quitRequested(quit, cmd) {
				t.Errorf("ctrl+c twice under the %s did not quit", pane.name)
			}
		})
	}
}

// ctrl+l under any modal pane repaints (tea.ClearScreen) and leaves the pane exactly as it stood.
func TestFrameChordCtrlLRepaintsUnderEveryModalPane(t *testing.T) {
	t.Parallel()
	for _, pane := range frameChordPanes() {
		t.Run(pane.name, func(t *testing.T) {
			t.Parallel()
			m := pane.open(t)
			before := pane.state(m)

			next, cmd := stepCmd(t, m, keyCtrlL())

			if got, want := cmdMsg(cmd), tea.ClearScreen(); !reflect.DeepEqual(got, want) {
				t.Errorf("ctrl+l under the %s produced %#v, want tea.ClearScreen's Msg", pane.name, got)
			}
			if after := pane.state(next); !reflect.DeepEqual(after, before) {
				t.Errorf("ctrl+l changed the %s:\n got %+v\nwant %+v", pane.name, after, before)
			}
		})
	}
}

// The one thing a frame chord takes from /settings is the every-keypress contract's: a drag-selected
// span in the value field loses its highlight, and the rest of the pane stands.
func TestFrameChordDropsTheSettingsDragSelection(t *testing.T) {
	t.Parallel()
	for _, chord := range []tea.KeyPressMsg{keyCtrlC(), keyCtrlL()} {
		t.Run(chord.String(), func(t *testing.T) {
			t.Parallel()
			m := openSettingsStep(t, settingsSubStepOf(t, settingsValueBuffer))
			m.settings.sel.seat(0, cell{})
			m.settings.sel.extend(3, cell{})
			want := m.settings
			want.sel = fieldSel{}

			next := step(t, m, chord)

			if next.settings.sel.active {
				t.Errorf("%s left the value field's drag-selection standing", chord.String())
			}
			if !reflect.DeepEqual(next.settings, want) {
				t.Errorf("%s changed the pane beyond its selection:\n got %+v\nwant %+v", chord.String(), next.settings, want)
			}
		})
	}
}

// The approval pane's rule field takes every key but the frame chords: ctrl+l repaints and ctrl+c
// arms the quit, and the field stays open holding what it held.
func TestFrameChordPassesTheApprovalRuleField(t *testing.T) {
	t.Parallel()
	m, _, _ := projectRulePane(t, terminalGate("go test ./..."), nil)
	m = step(t, m, keyRune('p'))
	value := m.ruleEdit.field.value()

	repainted, cmd := stepCmd(t, m, keyCtrlL())
	armed := step(t, repainted, keyCtrlC())

	if got, want := cmdMsg(cmd), tea.ClearScreen(); !reflect.DeepEqual(got, want) {
		t.Errorf("ctrl+l in the rule field produced %#v, want tea.ClearScreen's Msg", got)
	}
	if armed.lastCtrlC.IsZero() {
		t.Error("ctrl+c in the rule field did not arm the quit")
	}
	if !armed.editingProjectRule() {
		t.Fatal("a frame chord closed the rule field")
	}
	if got := armed.ruleEdit.field.value(); got != value {
		t.Errorf("the rule field holds %q after the frame chords, want %q", got, value)
	}
}
