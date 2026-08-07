package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
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
	m := step(t, newModel(context.Background(), eng, opts, nil), tea.WindowSizeMsg{Width: 80, Height: 24})
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
	th := newTheme(scheme.Default())
	modes := []domain.Mode{domain.ModePlan, domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto}
	seen := map[string]domain.Mode{}
	for _, mode := range modes {
		key := fmt.Sprintf("%v", th.modeColor(mode))
		if prev, dup := seen[key]; dup {
			t.Errorf("modeColor(%q) == modeColor(%q) == %s; want a distinct colour per mode", mode, prev, key)
		}
		seen[key] = mode
	}
}

// TestFooterModeMarkerLeadsWithTheModeSymbol pins the footer's mode marker on every rung of the
// ladder: the word is led by that rung's own glyph — ⊞ plan, ◐ ask before, ✔ allow edits, ▸▸ auto
// — and the glyph is part of the SAME styled run as the word rather than a separately coloured
// badge beside it. The styled assertion is the point: a glyph rendered under its own style would
// read identically once the escapes are stripped, and is the exact defect this forbids.
func TestFooterModeMarkerLeadsWithTheModeSymbol(t *testing.T) {
	for _, tc := range []struct {
		mode          domain.Mode
		symbol, label string
	}{
		{domain.ModePlan, "⊞", "plan"},
		{domain.ModeAskBefore, "◐", "ask before"},
		{domain.ModeAllowEdits, "✔", "allow edits"},
		{domain.ModeAuto, "▸▸", "auto"},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			m, _ := newModeModel(t, tc.mode)
			footer := m.footerContent(80)
			want := tc.symbol + " " + tc.label

			if got := modeMarker(tc.mode); got != want {
				t.Errorf("modeMarker(%q) = %q, want %q", tc.mode, got, want)
			}
			// The marker still ends the line bodyIndent short of the edge — the symbol joined the
			// slot, it did not displace the word from the column the gauge above it ends in.
			if flat := ansiPattern.ReplaceAllString(footer, ""); !strings.HasSuffix(flat, want+bodyIndent) {
				t.Errorf("footer = %q, want it to end %q", flat, want+bodyIndent)
			}
			if run := m.th.footerText.Foreground(m.th.modeColor(tc.mode)).Render(want); !strings.Contains(footer, run) {
				t.Errorf("footer does not carry %q as ONE styled run in the mode's own colour: %q", want, footer)
			}
		})
	}
}

// TestModeMarkerFallsBackToTheWordAlone proves an off-ladder mode keeps its word and borrows no
// other rung's shape: no glyph, and no orphan leading space where one would have gone.
func TestModeMarkerFallsBackToTheWordAlone(t *testing.T) {
	off := domain.Mode("hands-off")
	if got := modeSymbol(off); got != "" {
		t.Errorf("modeSymbol(%q) = %q, want no glyph for an off-ladder mode", off, got)
	}
	if got, want := modeMarker(off), string(off); got != want {
		t.Errorf("modeMarker(%q) = %q, want %q", off, got, want)
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
