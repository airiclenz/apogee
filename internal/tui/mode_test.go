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

// TestFooterDropsSegmentsBeforeTheModeMarker is the fit's pin on the row a human actually reads.
// The footer's one job on a narrow window is to keep saying which blast radius the session runs in,
// and the old shape had it exactly backwards: the marker dropped WHOLE the moment both ends did not
// fit, so the fact that matters most was the first to go. The row now spends its columns in the
// order it is read for — the effort word first, then the workdir, then the host — and the marker
// stays whole at every width in this sweep, Auto's blast-radius word beside it.
//
// The left run is asserted VERBATIM at each width, not merely searched: a segment that leaves takes
// its separator with it, so no rung of the ladder may open or close on a dangling ✦.
func TestFooterDropsSegmentsBeforeTheModeMarker(t *testing.T) {
	m := footerFactsModel(t)
	marker := footerModeText(modeMarker(domain.ModeAuto), confinedWord)

	for _, tc := range []struct {
		width int
		want  string
	}{
		{120, "test-host ✦ test-model ✦ high ✦ /ws/proj"}, // everything the session knows
		{80, "test-host ✦ test-model ✦ high ✦ /ws/proj"},
		{60, "test-host ✦ test-model ✦ /ws/proj"}, // priority 3: the effort word
		{40, "test-model"}, // then the workdir, then the host
	} {
		t.Run(fmt.Sprintf("width %d", tc.width), func(t *testing.T) {
			flat := ansiPattern.ReplaceAllString(m.footerContent(tc.width), "")

			if !strings.HasSuffix(flat, marker+bodyIndent) {
				t.Fatalf("footer = %q, want it to end %q — the marker is what the row never gives up",
					flat, marker+bodyIndent)
			}
			left := strings.TrimSpace(strings.TrimSuffix(flat, marker+bodyIndent))
			if left != tc.want {
				t.Errorf("footer's left run = %q, want %q", left, tc.want)
			}
			if strings.HasPrefix(left, glyphAssistant) || strings.HasSuffix(left, glyphAssistant) {
				t.Errorf("footer's left run = %q, want no dangling %q", left, glyphAssistant)
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
// ladder: the word is led by that rung's own glyph — ⊞ plan, ◐ ask before, ✔ allow edits, ⏵⏵ auto
// — and the glyph is part of the SAME styled run as the word rather than a separately coloured
// badge beside it. The styled assertion is the point: a glyph rendered under its own style would
// read identically once the escapes are stripped, and is the exact defect this forbids.
//
// Auto's marker now carries a second word — the blast radius (confinementWord) — so the rung whose
// marker grew states its tail here: a freshly built fake engine confines nothing, so the word is
// "unconfined" and it trails the marker's own styled run in the error tone. The symbol still leads,
// which is what this test is for.
func TestFooterModeMarkerLeadsWithTheModeSymbol(t *testing.T) {
	for _, tc := range []struct {
		mode                domain.Mode
		symbol, label, tail string
	}{
		{domain.ModePlan, "⊞", "plan", ""},
		{domain.ModeAskBefore, "◐", "ask before", ""},
		{domain.ModeAllowEdits, "✔", "allow edits", ""},
		{domain.ModeAuto, "⏵⏵", "auto", " · " + unconfinedWord},
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
			end := want + tc.tail + bodyIndent
			if flat := ansiPattern.ReplaceAllString(footer, ""); !strings.HasSuffix(flat, end) {
				t.Errorf("footer = %q, want it to end %q", flat, end)
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

// TestConfinementWordFollowsModeFlagAndBackend pins the footer's confinement word to the three
// cells Auto actually has, and to silence everywhere else. The rung is the gate: confinement
// attaches to Auto alone (ADR 0012), so the lower three say nothing about it however the flag and
// the backend stand — a word there would name a fence nothing reads. Inside Auto the flag outranks
// the backend ("unconfined" is the user's own decision, capable host or not) and the backend
// decides the remaining two: a fence it can keep reads "confined", one it cannot reads "gated" —
// the same word probe.DegradedNotice uses for that host.
func TestConfinementWordFollowsModeFlagAndBackend(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		info    ConfinementInfo
		mode    domain.Mode
		confine bool
		want    string
	}{
		{"auto confined on a fencing backend", capableHost, domain.ModeAuto, true, "confined"},
		{"auto gated where the backend cannot fence", degradedHost, domain.ModeAuto, true, "gated"},
		{"auto unconfined by the user's own decision", capableHost, domain.ModeAuto, false, "unconfined"},
		{"auto unconfined outranks a degraded backend", degradedHost, domain.ModeAuto, false, "unconfined"},
		{"auto with no backend wired reads as gated", ConfinementInfo{}, domain.ModeAuto, true, "gated"},
		{"allow edits says nothing", capableHost, domain.ModeAllowEdits, true, ""},
		{"ask before says nothing", capableHost, domain.ModeAskBefore, false, ""},
		{"plan says nothing", degradedHost, domain.ModePlan, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := confinementWord(tc.info, tc.mode, tc.confine); got != tc.want {
				t.Errorf("confinementWord(%+v, %q, confine=%v) = %q, want %q",
					tc.info, tc.mode, tc.confine, got, tc.want)
			}
		})
	}
}
