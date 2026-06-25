package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// keyShiftTab is the autonomy-mode cycle chord; its String() is "shift+tab", which handleKey
// matches (mirroring the textarea's "shift+enter" binding).
func keyShiftTab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift} }

// newModeModel builds an idle, sized model starting in mode `start`, returning it with the
// fakeEngine so a test can assert SetMode was driven on the engine.
func newModeModel(t *testing.T, start domain.Mode) (Model, *fakeEngine) {
	t.Helper()
	eng := &fakeEngine{}
	opts := testOpts
	opts.Mode = start
	m := step(t, newModel(context.Background(), eng, opts), tea.WindowSizeMsg{Width: 80, Height: 24})
	return m, eng
}

// TestModelShiftTabCyclesMode walks the full ladder (incl. the Auto→Plan wrap): each press
// advances opts.Mode, drives the engine via SetMode, and renders the FRIENDLY footer label
// (spaced, not the hyphenated wire form).
func TestModelShiftTabCyclesMode(t *testing.T) {
	cases := []struct {
		start, want domain.Mode
		label       string
	}{
		{domain.ModePlan, domain.ModeAskBefore, "ask before"},
		{domain.ModeAskBefore, domain.ModeAllowEdits, "allow edits"},
		{domain.ModeAllowEdits, domain.ModeAuto, "auto"},
		{domain.ModeAuto, domain.ModePlan, "plan"}, // wrap-around
	}
	for _, tc := range cases {
		t.Run(string(tc.start), func(t *testing.T) {
			m, eng := newModeModel(t, tc.start)

			m = step(t, m, keyShiftTab())

			if m.opts.Mode != tc.want {
				t.Fatalf("opts.Mode = %q, want %q", m.opts.Mode, tc.want)
			}
			if got := eng.modesSet(); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("engine SetMode = %v, want [%q]", got, tc.want)
			}
			footer := ansiPattern.ReplaceAllString(m.footerContent(80), "")
			if !strings.Contains(footer, tc.label) {
				t.Fatalf("footer = %q, want friendly label %q", footer, tc.label)
			}
			if canon := string(tc.want); strings.Contains(canon, "-") && strings.Contains(footer, canon) {
				t.Fatalf("footer shows hyphenated %q, want friendly label %q", canon, tc.label)
			}
		})
	}
}

// TestModeColorDistinct proves each autonomy mode maps to its own footer-marker colour, so the
// four markers are visually distinguishable.
func TestModeColorDistinct(t *testing.T) {
	modes := []domain.Mode{domain.ModePlan, domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto}
	seen := map[string]domain.Mode{}
	for _, mode := range modes {
		key := fmt.Sprintf("%v", modeColor(mode))
		if prev, dup := seen[key]; dup {
			t.Errorf("modeColor(%q) == modeColor(%q) == %s; want a distinct colour per mode", mode, prev, key)
		}
		seen[key] = mode
	}
}

// TestModelShiftTabCyclesWhileBusy proves mid-turn switching: Shift+Tab cycles the mode and
// drives the engine while running, while awaiting an approval, and while awaiting an answer.
func TestModelShiftTabCyclesWhileBusy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state uiState
	}{
		{"running", stateRunning},
		{"awaiting-approval", stateAwaitingApproval},
		{"awaiting-ask", stateAwaitingAsk},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, eng := newModeModel(t, domain.ModeAskBefore)
			m.state = tc.state

			m = step(t, m, keyShiftTab())

			if m.opts.Mode != domain.ModeAllowEdits {
				t.Fatalf("opts.Mode = %q, want allow-edits (cycle must work mid-turn)", m.opts.Mode)
			}
			if got := eng.modesSet(); len(got) != 1 || got[0] != domain.ModeAllowEdits {
				t.Fatalf("engine SetMode = %v, want [allow-edits]", got)
			}
		})
	}
}
