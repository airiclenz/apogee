package tui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/sidepanel"
	"github.com/airiclenz/apogee/internal/testtime"
)

// navModel builds a ready model for navigation mode (ADR 0111): the side panel docked beside the
// transcript (auto, 140 columns × 30) under the section list sections, its panel on a clock fixed
// at panelNow, events folded through the view's own Event fold, and the engine it drives handed
// back so a test can read what a key asked of it. The cursor fixtures elsewhere run with the
// column hidden; this is the one that shows it.
func navModel(t *testing.T, sections domain.SidePanelSections, events ...domain.Event) (Model, *fakeEngine) {
	t.Helper()

	eng := &fakeEngine{}
	opts := testOpts
	opts.UI.SidePanelShow = domain.SidePanelShowAuto
	opts.UI.SidePanelSections = sections
	m := newModel(context.Background(), eng, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
	m.panel = sidepanel.New(func() time.Time { return panelNow })
	for _, e := range events {
		m = m.foldEvent(e)
	}
	m.refreshViewport()
	if !m.panelShown() {
		t.Fatal("setup: the side panel does not show at 140 columns")
	}
	return m, eng
}

// onlySections is a section list showing only the sections named in shown, each auto, the rest hidden.
func onlySections(shown ...domain.SidePanelSection) domain.SidePanelSections {
	all := []domain.SidePanelSection{
		domain.SidePanelSubAgents, domain.SidePanelTasks, domain.SidePanelChangedFiles, domain.SidePanelUsage,
	}
	parts := make([]string, 0, len(all))
	for _, section := range all {
		mode := "hidden"
		if slices.Contains(shown, section) {
			mode = "auto"
		}
		parts = append(parts, string(section)+": "+mode)
	}
	return domain.SidePanelSections("[" + strings.Join(parts, ", ") + "]")
}

// panelCursorStop is the panel stop the cursor stands on, the mode and the region asserted on the way.
func panelCursorStop(t *testing.T, m Model) panelStop {
	t.Helper()
	if !m.cursor.inPanel() {
		t.Fatalf("the cursor does not stand in the panel (active %v, region %v)", m.cursor.active, m.cursor.region)
	}
	stops := m.navPanelStops()
	at, ok := m.cursor.panelStopIndex(stops)
	if !ok {
		t.Fatalf("the panel cursor %+v resolves to no stop of %+v", m.cursor.panel, stops)
	}
	return stops[at]
}

// transcriptCursorLine is the transcript line the cursor stands on, the region asserted on the way.
func transcriptCursorLine(t *testing.T, m Model) int {
	t.Helper()
	if !m.cursor.inTranscript() {
		t.Fatalf("the cursor does not stand in the transcript (active %v, region %v)", m.cursor.active, m.cursor.region)
	}
	return m.cursor.line
}

// TestNavModeRightEntersPanelLeftReturns is the move between the regions (ADR 0111 D1–2): → lands on
// the panel stop nearest the transcript row it leaves, ↑/↓ walk the column clamped at its ends, ←
// comes back to the transcript row it left, and a second → returns to the stop it remembers rather
// than the nearest one.
func TestNavModeRightEntersPanelLeftReturns(t *testing.T) {
	t.Parallel()
	m, _ := navModel(t, domain.DefaultSidePanelSections, panelStopEvents(t)...)
	m = step(t, m, keyAltUp())
	line := transcriptCursorLine(t, m)
	row := m.blockCursorRow()
	stops := m.navPanelStops()

	m = step(t, m, keyRight())

	entered := panelCursorStop(t, m)
	for _, stop := range stops {
		if abs(stop.y-row) < abs(entered.y-row) {
			t.Errorf("→ from row %d landed on the stop at row %d; the stop at row %d is nearer", row, entered.y, stop.y)
		}
	}
	m = step(t, m, keyUp())
	walked := panelCursorStop(t, m)
	if at := slices.IndexFunc(stops, func(s panelStop) bool { return s.id == entered.id }); at > 0 && walked.id != stops[at-1].id {
		t.Errorf("↑ in the panel moved to %+v, want the stop above, %+v", walked.id, stops[at-1].id)
	}
	for range len(stops) + 1 {
		m = step(t, m, keyUp())
	}
	if top := panelCursorStop(t, m); top.id != stops[0].id {
		t.Errorf("↑ past the column's top stands on %+v, want the first stop %+v (clamped)", top.id, stops[0].id)
	}
	for range len(stops) + 1 {
		m = step(t, m, keyAltDown())
	}
	if bottom := panelCursorStop(t, m); bottom.id != stops[len(stops)-1].id {
		t.Errorf("⌥↓ past the column's bottom stands on %+v, want the last stop %+v (clamped)", bottom.id, stops[len(stops)-1].id)
	}
	m = step(t, m, keyUp())
	remembered := panelCursorStop(t, m)

	m = step(t, m, keyLeft())

	if got := transcriptCursorLine(t, m); got != line {
		t.Errorf("← returned to line %d, want the transcript row it left, %d", got, line)
	}
	m = step(t, m, keyRight())
	if got := panelCursorStop(t, m); got.id != remembered.id {
		t.Errorf("the second → stands on %+v, want the remembered stop %+v", got.id, remembered.id)
	}
}

// TestNavModeEntryLandsInPanelWithoutTranscriptStops is ADR 0111 D3's way in: with nothing in the
// transcript to stand on, ⌥↑ and ⌥↓ from the prompt land in the panel, each at the end of the column
// it points away from.
func TestNavModeEntryLandsInPanelWithoutTranscriptStops(t *testing.T) {
	t.Parallel()
	base, _ := navModel(t, domain.DefaultSidePanelSections, taskList(0, "one", "two"), panelUsageReading(panelUsed))
	if stops := cursorStops(base.lineTargets, base.pads); len(stops) != 0 {
		t.Fatalf("setup: the transcript offers stops %v; want none", stops)
	}
	stops := base.navPanelStops()
	if len(stops) < 2 {
		t.Fatalf("setup: the column offers %d stops, too few to tell its ends apart", len(stops))
	}

	if got := panelCursorStop(t, step(t, base, keyAltUp())); got.id != stops[len(stops)-1].id {
		t.Errorf("⌥↑ entered the panel at %+v, want its last stop %+v", got.id, stops[len(stops)-1].id)
	}
	if got := panelCursorStop(t, step(t, base, keyAltDown())); got.id != stops[0].id {
		t.Errorf("⌥↓ entered the panel at %+v, want its first stop %+v", got.id, stops[0].id)
	}
	if got := step(t, step(t, base, keyAltUp()), keyLeft()); !got.cursor.inPanel() {
		t.Error("← left the panel for a transcript with no stop to stand on")
	}
}

// TestNavModeArrowsLeaveThePromptCaret pins ← and → as the mode's in either region, even where they
// have nowhere to go: the prompt's caret and draft never move under a reader who is navigating.
func TestNavModeArrowsLeaveThePromptCaret(t *testing.T) {
	t.Parallel()
	m, _ := navModel(t, domain.DefaultSidePanelSections, panelStopEvents(t)...)
	m = typeDraft(t, m, "hello")
	m = step(t, m, keyLeft())
	m = step(t, m, keyLeft())
	value, column := m.input.Value(), m.input.Column()

	m = step(t, m, keyAltUp())
	for _, key := range []tea.KeyPressMsg{keyLeft(), keyRight(), keyRight(), keyLeft(), keyLeft()} {
		m = step(t, m, key)
	}

	if !m.cursor.active {
		t.Fatal("an arrow ended navigation mode")
	}
	if got := m.input.Value(); got != value {
		t.Errorf("the draft reads %q, want %q untouched", got, value)
	}
	if got := m.input.Column(); got != column {
		t.Errorf("the caret moved to column %d, want it left at %d", got, column)
	}
}

// TestNavModeEscLeavesFromEitherRegion pins esc as the way out of the mode from either region — and,
// inside a Run view, the split between them: esc on the view's transcript goes one level up, as it
// always has, while esc in the panel leaves only the mode and the view stays open.
func TestNavModeEscLeavesFromEitherRegion(t *testing.T) {
	t.Parallel()
	inView := func(t *testing.T) Model {
		t.Helper()
		m, _ := navModel(t, domain.DefaultSidePanelSections, panelStopEvents(t)...)
		m = clickCell(t, m, panelBodyX(m), panelRowY(t, m, "scout"))
		if !m.inRunView() {
			t.Fatal("setup: the sub-agent row did not open its Run view")
		}
		return m
	}
	cases := []struct {
		name     string
		build    func(t *testing.T) Model
		keys     []tea.KeyPressMsg
		inPanel  bool
		wantView bool
	}{
		{"transcript", func(t *testing.T) Model {
			m, _ := navModel(t, domain.DefaultSidePanelSections, panelStopEvents(t)...)
			return m
		}, []tea.KeyPressMsg{keyAltUp()}, false, false},
		{"panel", func(t *testing.T) Model {
			m, _ := navModel(t, domain.DefaultSidePanelSections, panelStopEvents(t)...)
			return m
		}, []tea.KeyPressMsg{keyAltUp(), keyRight()}, true, false},
		{"transcript inside a Run view goes one level up", inView, []tea.KeyPressMsg{keyAltUp()}, false, false},
		{"panel inside a Run view leaves only the mode", inView, []tea.KeyPressMsg{keyAltUp(), keyRight()}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := tc.build(t)
			for _, key := range tc.keys {
				m = step(t, m, key)
			}
			if m.cursor.inPanel() != tc.inPanel || !m.cursor.active {
				t.Fatalf("setup: cursor active %v in the panel %v; want in the panel %v", m.cursor.active, m.cursor.inPanel(), tc.inPanel)
			}
			wasInView := m.inRunView()

			m = step(t, m, keyEsc())

			if m.cursor.active {
				t.Error("esc left navigation mode on")
			}
			if wasInView && m.inRunView() != tc.wantView {
				t.Errorf("after esc the Run view is open %v, want %v", m.inRunView(), tc.wantView)
			}
		})
	}
}

// TestNavModeRightIntoEmptyPanelDoesNothing pins → where the panel has no stop — a column showing
// `nothing live`, or one the window is too narrow to show: the cursor stays on the transcript row it
// stands on, and the key is still the mode's.
func TestNavModeRightIntoEmptyPanelDoesNothing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		build func(t *testing.T) Model
	}{
		{"a column with no stop", func(t *testing.T) Model {
			m, _ := navModel(t, onlySections(domain.SidePanelTasks), panelWrite(domain.EventBase{}, "w1", "/w/top.go")...)
			if stops := m.navPanelStops(); len(stops) != 0 {
				t.Fatalf("setup: the column offers stops %+v; want none", stops)
			}
			return m
		}},
		{"a column the window cannot show", func(t *testing.T) Model {
			m, _ := navModel(t, domain.DefaultSidePanelSections, panelStopEvents(t)...)
			m = step(t, m, tea.WindowSizeMsg{Width: 60, Height: 30})
			if m.panelShown() {
				t.Fatal("setup: the column still shows at 60 columns")
			}
			return m
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := step(t, tc.build(t), keyAltUp())
			line := transcriptCursorLine(t, m)

			next, _, claimed := m.blockCursorKey(keyRight())

			if !claimed {
				t.Error("→ was left to the prompt's caret")
			}
			if got := transcriptCursorLine(t, next); got != line {
				t.Errorf("→ moved the cursor to line %d, want it left on %d", got, line)
			}
		})
	}
}

// TestNavModeRegionLosingStops pins the handoff when a region loses every stop: the transcript's
// cursor goes to the panel when the transcript empties, the panel's goes back to the transcript when
// the column empties — judged at the next key, as the sub-agents section's linger can end with none
// pressed — and the mode ends where the other region has no stop either.
func TestNavModeRegionLosingStops(t *testing.T) {
	t.Parallel()

	t.Run("the panel empties at the linger's end, judged at the next key", func(t *testing.T) {
		t.Parallel()
		m, _ := navModel(t, onlySections(domain.SidePanelSubAgents))
		clock := panelNow
		m.panel = sidepanel.New(func() time.Time { return clock })
		finished := domain.SubAgentPhaseEvent{
			EventBase: delegationBase("r1"), Phase: domain.SubAgentFinished, At: panelNow.Add(-testtime.Fixed(10 * time.Second)),
		}
		for _, e := range []domain.Event{panelDelegationCall(t, "r1", "scout"), delegationPhase("r1", domain.SubAgentStarted, time.Minute), finished} {
			m = step(t, m, eventMsg{Event: e})
		}
		m = step(t, m, keyAltUp())
		line := transcriptCursorLine(t, m)
		m = step(t, m, keyRight())
		panelCursorStop(t, m)

		clock = panelNow.Add(testtime.Fixed(time.Hour))
		if stops := m.navPanelStops(); len(stops) != 0 {
			t.Fatalf("setup: the column still offers %+v past the linger", stops)
		}
		m = step(t, m, keyDown())

		if got := transcriptCursorLine(t, m); got != line {
			t.Errorf("the handed-back cursor stands on line %d, want the transcript row it left, %d", got, line)
		}
	})
	t.Run("the panel empties and the transcript has no stop: the mode ends", func(t *testing.T) {
		t.Parallel()
		m, _ := navModel(t, domain.DefaultSidePanelSections, panelUsageReading(panelUsed))
		m = step(t, m, keyAltUp())
		panelCursorStop(t, m)
		m = step(t, m, tea.WindowSizeMsg{Width: 60, Height: 30})

		m = step(t, m, keyDown())

		if m.cursor.active {
			t.Error("the mode stayed on with no stop in either region")
		}
	})
	t.Run("the transcript empties: handed to the panel", func(t *testing.T) {
		t.Parallel()
		m, _ := navModel(t, domain.DefaultSidePanelSections, append(panelWrite(domain.EventBase{}, "w1", "/w/top.go"), panelUsageReading(panelUsed))...)
		m = step(t, m, keyAltUp())
		transcriptCursorLine(t, m)

		m.transcript.entries = nil
		m.refreshViewport()

		stops := m.navPanelStops()
		if got := panelCursorStop(t, m); len(stops) == 0 || got.id != stops[0].id {
			t.Errorf("the handed cursor stands on %+v, want the column's first stop", got.id)
		}
	})
	t.Run("the transcript empties and the panel has no stop: the mode ends", func(t *testing.T) {
		t.Parallel()
		m, _ := navModel(t, onlySections(domain.SidePanelTasks), panelWrite(domain.EventBase{}, "w1", "/w/top.go")...)
		m = step(t, m, keyAltUp())
		transcriptCursorLine(t, m)

		m.transcript.entries = nil
		m.refreshViewport()

		if m.cursor.active {
			t.Error("the mode stayed on with no stop in either region")
		}
	})
}

// TestNavModePanelKeysLeaveTheTranscriptRow pins the remembered transcript place as only remembered
// while the cursor stands in the panel: ⏎ there opens no block on that row, and `^x` stops no run
// whose row it is.
func TestNavModePanelKeysLeaveTheTranscriptRow(t *testing.T) {
	t.Parallel()
	m, eng := navModel(t, domain.DefaultSidePanelSections, panelStopEvents(t)...)
	m = cursorOnRun(t, m, "r1")
	flags := expandedFlags(m)
	m = step(t, m, keyRight())
	panelCursorStop(t, m)

	m = step(t, m, keyEnter())
	m = step(t, m, keyCtrlX())

	if got := eng.stoppedChildren(); len(got) != 0 {
		t.Errorf("StopChild calls = %v; `^x` in the panel stopped the remembered transcript row's run", got)
	}
	if m.inRunView() {
		t.Error("⏎ in the panel opened the remembered transcript row's Run view")
	}
	if got := expandedFlags(m); !slices.Equal(got, flags) {
		t.Errorf("expanded flags %v, want %v: a key in the panel toggled a transcript block", got, flags)
	}
}

// TestNavModePanelHighlightYieldsToADecision pins the panel cursor's paint: its stop's row wears the
// selection tone and the transcript's remembered row does not, and — as the transcript's bar does —
// the highlight goes with the keys while an approval decision is pending.
func TestNavModePanelHighlightYieldsToADecision(t *testing.T) {
	t.Parallel()
	m, _ := navModel(t, domain.DefaultSidePanelSections, panelStopEvents(t)...)
	if !colorActive(m.th) {
		t.Skip("this profile emits no styling to assert the selection tone by")
	}
	bar := styleSGR(m.th.selection)
	m = step(t, m, keyAltUp())
	m = step(t, m, keyRight())
	stop := panelCursorStop(t, m)

	lines := strings.Split(m.View().Content, "\n")
	for i, line := range lines {
		if got := strings.Contains(line, bar); got != (i == stop.y) {
			t.Errorf("row %d carries the selection tone %v, want it on the panel cursor's row %d alone: %q", i, got, stop.y, strip(line))
		}
	}

	reply := make(chan domain.ApprovalDecision, 1)
	m = step(t, m, approvalReqMsg{Request: domain.ApprovalRequest{Tool: "terminal"}, Reply: reply})
	m = armApproval(t, m)
	row := strings.Split(m.View().Content, "\n")[stop.y]
	if column := m.th.measure.Cut(row, m.slotWidth(), m.width); strings.Contains(column, bar) {
		t.Errorf("the panel cursor's row is still painted while an approval decision is pending: %q", strip(column))
	}
	if _, _, claimed := m.blockCursorKey(keyDown()); claimed {
		t.Error("navigation mode claimed ↓ while an approval decision was pending")
	}
}
