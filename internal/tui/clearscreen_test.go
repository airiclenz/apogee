package tui

import (
	"reflect"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// ctrl+l — the full-repaint escape hatch (handleKey)
// ----------------------------------------------------------------------------

// keyCtrlL is the repaint chord; its String() is "ctrl+l", which handleKey matches (the keyCtrlC
// shape).
func keyCtrlL() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl} }

// repaintSnapshot is everything about the Model ctrl+l must leave alone. It is a comparable value
// so the assertion is one `==` rather than a field-by-field walk, and it deliberately spans the
// four things a stray key could plausibly disturb: what state the machine is in, what is in the
// box, where the transcript is parked, and what the chrome is saying.
type repaintSnapshot struct {
	state         uiState
	value         string
	scroll        int
	entries       int
	pending       string
	flash         string
	detached      bool
	ready         bool
	width, height int
	queued        int
}

// snapshotRepaint reads the fields ctrl+l must not touch off a Model.
func snapshotRepaint(m Model) repaintSnapshot {
	return repaintSnapshot{
		state:    m.state,
		value:    m.input.Value(),
		scroll:   m.viewport.YOffset(),
		entries:  len(m.transcript.entries),
		pending:  m.transcript.pending.String(),
		flash:    m.flash,
		detached: m.detached,
		ready:    m.ready,
		width:    m.width,
		height:   m.height,
		queued:   len(m.pendingInterjections),
	}
}

// TestCtrlLKeyReturnsClearScreen is the binding itself: ctrl+l hands back bubbletea's clear-screen
// Cmd — which drives the renderer's MoveTo(0,0) + Erase(), so the next frame is a full repaint
// rather than a diff — and changes nothing else about the Model. Asserted at idle AND while a
// worker runs, because the repair is worth least when the screen is still and most when it is
// streaming.
func TestCtrlLKeyReturnsClearScreen(t *testing.T) {
	cases := []struct {
		name  string
		build func(*testing.T) Model
	}{
		{"idle", newTestModel},
		{"running", runningModel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.build(t)
			m.input.SetValue("half a thought") // a draft the repaint must not eat
			before := snapshotRepaint(m)

			next, cmd := stepCmd(t, m, keyCtrlL())

			if cmd == nil {
				t.Fatal("ctrl+l returned no Cmd; want bubbletea's clear-screen Cmd")
			}
			// clearScreenMsg is unexported, so the Msg tea.ClearScreen itself produces IS the
			// expectation — nothing else can name the type.
			if got, want := cmdMsg(cmd), tea.ClearScreen(); !reflect.DeepEqual(got, want) {
				t.Fatalf("ctrl+l Cmd produced %#v, want %#v (tea.ClearScreen's Msg)", got, want)
			}
			if after := snapshotRepaint(next); after != before {
				t.Fatalf("ctrl+l changed the Model:\n got %+v\nwant %+v", after, before)
			}
		})
	}
}

// TestCtrlLKeyLeavesTheFrameIdentical is the other half of "display command and nothing else":
// the frame that lands after the repaint is byte-for-byte the frame that was already there. Idle
// only — the running status line carries an elapsed clock, so two renders of it are equal only
// until the second ticks over.
func TestCtrlLKeyLeavesTheFrameIdentical(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("half a thought")
	before := m.View().Content

	next := step(t, m, keyCtrlL())

	if after := next.View().Content; after != before {
		t.Fatalf("ctrl+l changed the rendered frame:\n got %q\nwant %q", plain(next.View()), plain(m.View()))
	}
}

// TestCtrlLKeyStealsNothingFromThePromptEditor pins the premise the binding rests on: the chat
// field claims no ctrl+l of its own, so intercepting it globally takes nothing away from the
// human's editing vocabulary. Asserted against the field's whole KeyMap by reflection rather than
// against the handful of bindings this package sets, so a future bubbles release that DID bind the
// chord fails here instead of quietly losing it.
func TestCtrlLKeyStealsNothingFromThePromptEditor(t *testing.T) {
	m := newTestModel(t)
	km := reflect.ValueOf(m.input.KeyMap)
	bindingType := reflect.TypeOf(key.Binding{})
	checked := 0
	for i := range km.NumField() {
		field := km.Field(i)
		if field.Type() != bindingType {
			continue
		}
		checked++
		if binding, ok := field.Interface().(key.Binding); ok && key.Matches(keyCtrlL(), binding) {
			t.Errorf("textarea KeyMap.%s binds ctrl+l (%v); the global repaint chord would steal it",
				km.Type().Field(i).Name, binding.Keys())
		}
	}
	if checked == 0 {
		t.Fatal("walked no bindings; the KeyMap reflection is not reading the field's bindings")
	}

	// And the key really is inert in the box: a draft survives it untouched.
	m.input.SetValue("keep me")
	if next := step(t, m, keyCtrlL()); next.input.Value() != "keep me" {
		t.Errorf("input = %q after ctrl+l, want %q", next.input.Value(), "keep me")
	}
}

// TestCtrlLKeySwallowedByModalOverlay holds the modal contract: while a full-claim overlay is up it
// takes every key it does not act on, and ctrl+l is not one of the keys any of them act on. A
// repaint fired from under a modal would be harmless on screen but would break the rule the three
// panes share — the human's keyboard is the overlay's for as long as it is open — so the escape
// hatch waits for esc like everything else.
func TestCtrlLKeySwallowedByModalOverlay(t *testing.T) {
	cases := []struct {
		name string
		open func(Model) Model
		up   func(Model) bool
	}{
		{
			name: "sessions browser",
			open: func(m Model) Model { m.sessionBrowser = sessionBrowser{open: true}; return m },
			up:   func(m Model) bool { return m.sessionBrowser.open },
		},
		{
			name: "settings pane",
			open: func(m Model) Model { m.settings.open = true; return m },
			up:   func(m Model) bool { return m.settings.open },
		},
		{
			name: "picker",
			open: func(m Model) Model { m.picker = picker{open: true, kind: pickerModel}; return m },
			up:   func(m Model) bool { return m.picker.open },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.open(newTestModel(t))

			next, cmd := stepCmd(t, m, keyCtrlL())

			if cmd != nil {
				t.Errorf("ctrl+l returned a Cmd under the %s; the modal must swallow it", tc.name)
			}
			if !tc.up(next) {
				t.Errorf("ctrl+l closed the %s; it must leave the overlay standing", tc.name)
			}
		})
	}
}
