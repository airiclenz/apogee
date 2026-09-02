package tui

import (
	"fmt"
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The shared report pane — /usage, /inspect and /thinking through one body (reportpane.go)
// ----------------------------------------------------------------------------
//
// The assertions below drive the reports through the SHARED functions rather than through the names
// their own files give them, because what is under test is the one body those names share: a claim
// proved for reportKey is proved for both panes at once, which is the whole point of there being one.

// reportCase is one open report over more rows than its pane can seat — the only state in which a
// scroll means anything.
type reportCase struct {
	name  string
	kind  reportKind
	model Model
}

// reportCases opens one of each kind. Every one of them starts at the TOP with a full window still
// below it, so every step and every clamp below has somewhere to be seen.
func reportCases(t *testing.T) []reportCase {
	t.Helper()
	return []reportCase{
		{name: "/usage", kind: usageReport, model: usageReportModel(t, 40)},
		{name: "/inspect", kind: inspectReport, model: inspectorPaneModel(t, 40)},
		{name: "/thinking", kind: thinkingReport, model: thinkingPaneModel(t, 40)},
	}
}

// TestReportKindsResolveDistinctly is the guard the module's fall-through cost: every declared
// reportKind resolves to its OWN frame pane, its OWN state field and its OWN content. The three
// resolvers were once `if r == inspectReport {…}` with /usage as the fall-through, so a kind that
// missed a branch compiled and painted ANOTHER pane's state or rows inside its box — a wrong pane
// rather than a build error. Walking the kinds is what makes a fourth report inherit this guard.
func TestReportKindsResolveDistinctly(t *testing.T) {
	m := newTestModel(t)

	panes := map[framePane]reportKind{}
	states := map[*reportPane]reportKind{}
	titles := map[string]reportKind{}
	for r := reportKind(0); r < reportKinds; r++ {
		if other, seen := panes[r.pane()]; seen {
			t.Errorf("report %d and report %d share the frame pane %d", r, other, r.pane())
		}
		panes[r.pane()] = r

		state := m.reportState(r)
		if other, seen := states[state]; seen {
			t.Errorf("report %d and report %d share one state field: scrolling either would move both", r, other)
		}
		states[state] = r

		title := m.reportContent(r).title
		if title == "" {
			t.Errorf("report %d composes no title: its box would name nothing", r)
		}
		if other, seen := titles[title]; seen {
			t.Errorf("report %d and report %d both call themselves %q", r, other, title)
		}
		titles[title] = r
	}

	for want, name := range map[reportKind]string{
		usageReport:    usageTitle,
		inspectReport:  inspectorTitle,
		thinkingReport: thinkingTitle,
	} {
		if got := m.reportContent(want).title; got != name {
			t.Errorf("report %d is titled %q, want %q — the kind resolves to another pane's content", want, got, name)
		}
	}
}

// pressReport presses one key on the named report and returns the model it left, failing when the
// report did not claim the key at all.
func pressReport(t *testing.T, m Model, r reportKind, msg tea.KeyPressMsg) Model {
	t.Helper()
	handled, next, _ := m.reportKey(r, msg)
	if !handled {
		t.Fatalf("the report did not claim %q", msg.String())
	}
	return next.(Model)
}

// TestReportKeysScrollEitherReportByOneArithmetic pins the key contract both panes now answer
// through: ↑/↓ move a row, pgup/pgdown a DRAWN window, both clamp at the first row and at the last
// full window, and esc closes the report leaving no scroll behind for the next open.
func TestReportKeysScrollEitherReportByOneArithmetic(t *testing.T) {
	for _, tc := range reportCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.model
			win, ok := m.reportWindow(tc.kind)
			if !ok {
				t.Fatal("the open report reports no window")
			}
			seats := win.end - win.start
			if win.start != 0 || seats <= 0 || win.total < 2*seats {
				t.Fatalf("precondition: window [%d,%d) of %d rows — the report must open at the top with a full window below it",
					win.start, win.end, win.total)
			}

			t.Run("the arrows move one row and clamp at the first", func(t *testing.T) {
				down := pressReport(t, m, tc.kind, keyDown())
				if got := down.reportState(tc.kind).top; got != 1 {
					t.Fatalf("top = %d after ↓, want 1", got)
				}
				back := pressReport(t, pressReport(t, down, tc.kind, keyUp()), tc.kind, keyUp())
				if got := back.reportState(tc.kind).top; got != 0 {
					t.Errorf("top = %d after stepping past the first row, want it clamped at 0", got)
				}
			})

			t.Run("the page keys move a drawn window and clamp at the last full one", func(t *testing.T) {
				page := pressReport(t, m, tc.kind, keyPgDown())
				if got := page.reportState(tc.kind).top; got != seats {
					t.Errorf("top = %d after pgdn, want a full window of %d rows", got, seats)
				}
				top := pressReport(t, m, tc.kind, keyPgUp())
				if got := top.reportState(tc.kind).top; got != 0 {
					t.Errorf("top = %d after pgup at the top, want it clamped at 0", got)
				}

				end := m
				for range win.total {
					end = pressReport(t, end, tc.kind, keyPgDown())
				}
				last, ok := end.reportWindow(tc.kind)
				if !ok {
					t.Fatal("the scrolled report reports no window")
				}
				if last.end != last.total || last.end-last.start != seats {
					t.Errorf("paged to the end the window is [%d,%d) of %d rows, want a full %d ending on the last row",
						last.start, last.end, last.total, seats)
				}
			})

			t.Run("esc closes it and leaves nothing behind", func(t *testing.T) {
				closed := pressReport(t, pressReport(t, m, tc.kind, keyDown()), tc.kind, keyEsc())
				if state := closed.reportState(tc.kind); state.open || state.top != 0 {
					t.Errorf("after esc the report is %+v, want it closed at the first row", *state)
				}
			})

			t.Run("it claims nothing else, and nothing at all once closed", func(t *testing.T) {
				if handled, _, _ := m.reportKey(tc.kind, keyRune('x')); handled {
					t.Error("the report claimed a printable key — it is a report, not a modal")
				}
				closed := m.dismissReport(tc.kind)
				if handled, _, _ := closed.reportKey(tc.kind, keyPgDown()); handled {
					t.Error("a closed report claimed pgdn, leaving the transcript unscrollable behind nothing")
				}
			})
		})
	}
}

// TestReportWindowIsThePaintersOwnAnswer pins what both the keys and the wheel rest on: the window a
// report reports is the window the frame DREW — the spec's clamped top and the rows the painter
// seated from it — rather than a second derivation that could disagree with the paint by a row.
func TestReportWindowIsThePaintersOwnAnswer(t *testing.T) {
	for _, tc := range reportCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.model
			m.reportState(tc.kind).top = 2

			spec, seated := m.reportSpec(tc.kind, m.reportContent(tc.kind))
			if !seated {
				t.Fatal("the frame seated no pane for an open report")
			}
			win, ok := m.reportWindow(tc.kind)
			if !ok {
				t.Fatal("the open report reports no window")
			}
			if win.start != spec.rowTop || win.end-win.start != spec.maxRows || win.total != len(spec.rows) {
				t.Errorf("window [%d,%d) of %d, want the composed [%d,%d) of %d",
					win.start, win.end, win.total, spec.rowTop, spec.rowTop+spec.maxRows, len(spec.rows))
			}
		})
	}
}

// TestReportScrollClampsToTheLastFullWindow pins the correction a stale offset gets: a top left over
// from a taller window — or set past the end, which is where the /inspect verb deliberately opens
// (runInspectCommand) — composes the LAST FULL window rather than one row over an empty pane.
func TestReportScrollClampsToTheLastFullWindow(t *testing.T) {
	for _, tc := range reportCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.model
			m.reportState(tc.kind).top = 1 << 20 // far past the last row

			win, ok := m.reportWindow(tc.kind)
			if !ok {
				t.Fatal("the open report reports no window")
			}
			if win.end != win.total {
				t.Errorf("window [%d,%d) of %d rows, want it ending on the last row", win.start, win.end, win.total)
			}
			spec, _ := m.reportSpec(tc.kind, m.reportContent(tc.kind))
			if win.end-win.start != spec.maxRows {
				t.Errorf("the clamped window shows %d rows, want the full %d the frame granted",
					win.end-win.start, spec.maxRows)
			}
		})
	}
}

// TestTranscriptSlotPanesStateTheStackingOrderOnce pins the list every rectangle in the
// transcript-side slot is measured through: each pane of that slot is named in it exactly once, and
// the /inspect box therefore begins exactly where the /usage box ends. The two hand-written `above`
// slices this replaced already differed by one element — a pane that joined the frame without
// joining both of them was a bug neither of them could be read to find.
func TestTranscriptSlotPanesStateTheStackingOrderOnce(t *testing.T) {
	t.Parallel()

	named := map[framePane]int{}
	for _, p := range transcriptSlotPanes {
		named[p]++
	}
	for p := framePane(0); p < paneKinds; p++ {
		want := 1
		if p == paneDropdown {
			// The autocomplete dropdown is in the OTHER slot, hugging the input box, so it is no part
			// of this arithmetic. Every other pane of the frame must be.
			want = 0
		}
		if named[p] != want {
			t.Errorf("pane %d is named %d times in the slot's order, want %d", p, named[p], want)
		}
	}
}

// TestTheTwoReportRectsStackInTheSlotsStatedOrder is the geometric half of the claim above, on the
// one frame that draws both reports: the /inspect pane opens on the row the /usage report closes on,
// with nothing between them and nothing overlapping.
func TestTheTwoReportRectsStackInTheSlotsStatedOrder(t *testing.T) {
	m := bothPanesModel(t, 4)

	usageTop, usageRows, ok := m.reportPaneRect(usageReport)
	if !ok {
		t.Fatal("the report is not on the frame")
	}
	inspectTop, inspectRows, ok := m.reportPaneRect(inspectReport)
	if !ok {
		t.Fatal("the /inspect pane is not on the frame")
	}

	if inspectTop != usageTop+usageRows {
		t.Errorf("the /inspect box starts at row %d, want %d — directly under the report's %d rows",
			inspectTop, usageTop+usageRows, usageRows)
	}
	if inspectRows <= 0 {
		t.Errorf("the /inspect box is %d rows tall", inspectRows)
	}
}

// TestFrameOverlayBlocksAnswerForEveryPane pins the lookup the slot's order is walked through: every
// framePane resolves to its OWN block, so a pane whose field the lookup forgot could not be measured
// as an empty one and silently drop the rows it takes off every rectangle below it.
func TestFrameOverlayBlocksAnswerForEveryPane(t *testing.T) {
	t.Parallel()

	ov := frameOverlays{
		prompt:   "prompt",
		browser:  "browser",
		picker:   "picker",
		settings: "settings",
		usage:    "usage",
		dropdown: "dropdown",
	}
	ov.inspector = "inspector"
	ov.thinking = "thinking"

	for p, want := range map[framePane]string{
		panePrompt:    ov.prompt,
		paneBrowser:   ov.browser,
		panePicker:    ov.picker,
		paneSettings:  ov.settings,
		paneUsage:     ov.usage,
		paneInspector: ov.inspector,
		paneThinking:  ov.thinking,
		paneDropdown:  ov.dropdown,
	} {
		if got := ov.block(p); got != want {
			t.Errorf("pane %d resolves to %q, want %q", p, got, want)
		}
	}
}

// TestCtrlRIsTheInspectorsKeyAlone pins the module's ONE asymmetry: the shared body claims ctrl+r for
// /inspect, which has two renderings of every record, and never for /usage, which has one. A key
// claimed on both would swallow a chord the live box behind the /usage report was owed — the same
// reason the toggle is not a printable letter (the doctrine in reportpane.go).
func TestCtrlRIsTheInspectorsKeyAlone(t *testing.T) {
	if handled, _, _ := usageReportModel(t, 40).reportKey(usageReport, ctrlR()); handled {
		t.Error("/usage claimed ctrl+r; it has one rendering and the chord belongs to the box behind it")
	}

	handled, next, _ := inspectorPaneModel(t, 40).reportKey(inspectReport, ctrlR())
	if !handled {
		t.Fatal("/inspect did not claim ctrl+r")
	}
	if !next.(Model).inspector.raw {
		t.Error("ctrl+r did not flip /inspect to its raw rendering")
	}
}

// ----------------------------------------------------------------------------
// Following the tail (reportKind.follows)
// ----------------------------------------------------------------------------

// followCase is one report that FOLLOWS the tail of its rows, opened over more rows than the pane can
// seat, together with a way to append MORE than a full window of rows to the list behind it — which
// is the only state in which "does the pane move with them" is a question.
type followCase struct {
	name  string
	kind  reportKind
	model Model
	grow  func(t *testing.T, m Model) Model
}

// followCases opens each of the two reports that follow. /usage is not among them and has its own
// test below: it keeps the clamp alone (reportKind.follows).
func followCases(t *testing.T) []followCase {
	t.Helper()
	return []followCase{
		{
			name:  "/inspect",
			kind:  inspectReport,
			model: inspectorPaneModel(t, 6),
			// The ring holds maxWireRecords and rotates past it, which would take the oldest records
			// away as fast as the new ones arrive and leave the list no longer than it started: the
			// growth here stays UNDER that cap so there is real growth to follow.
			grow: func(t *testing.T, m Model) Model {
				t.Helper()
				return growInspectorRecords(t, m, 6)
			},
		},
		{
			name:  "/thinking",
			kind:  thinkingReport,
			model: thinkingPaneModel(t, 6),
			grow: func(t *testing.T, m Model) Model {
				t.Helper()
				return growThinkingRecords(t, m, 6)
			},
		},
	}
}

// growInspectorRecords folds wire records onto the ring until the pane's row list has grown by more
// than a full window, and fails when the ring's cap is reached first — past it the oldest record
// rotates out for every new one and the list stops growing, which would leave every claim below
// about "the rows that arrived" unprovable rather than false.
func growInspectorRecords(t *testing.T, m Model, first int) Model {
	t.Helper()
	spec, seated := m.reportSpec(inspectReport, m.reportContent(inspectReport))
	if !seated {
		t.Fatal("the frame seated no /inspect pane to grow under")
	}
	before := len(spec.rows)
	for i := first; ; i++ {
		if i >= maxWireRecords {
			t.Fatalf("the ring's %d-record cap was reached before the list grew a full window of %d rows",
				maxWireRecords, spec.maxRows)
		}
		m = m.foldEvent(wireEvent(domain.WireDirectionRequest, fmt.Sprintf(`{"n":%d}`, i), i, 0))
		if rows, _ := m.inspectorRows(); len(rows)-before > spec.maxRows {
			return m
		}
	}
}

// growThinkingRecords folds completed turns onto the board until its row list has grown by more than
// a full window. The board caps at maxThinkingRecords, far above what this needs.
func growThinkingRecords(t *testing.T, m Model, first int) Model {
	t.Helper()
	spec, seated := m.reportSpec(thinkingReport, m.reportContent(thinkingReport))
	if !seated {
		t.Fatal("the frame seated no /thinking pane to grow under")
	}
	before := len(spec.rows)
	for i := first; ; i++ {
		if i >= maxThinkingRecords {
			t.Fatalf("the board's %d-record cap was reached before the list grew a full window of %d rows",
				maxThinkingRecords, spec.maxRows)
		}
		m = m.foldEvent(reasoningAt(runRef{}, i, "record "+strconv.Itoa(i)+" reasoning"))
		m = m.foldEvent(domain.MessageEvent{EventBase: eventBaseAt(runRef{}, i)})
		if rows, _ := m.thinkingRows(m.thinkingWrapColumn()); len(rows)-before > spec.maxRows {
			return m
		}
	}
}

// reportWindowOrFail is the window the frame drew for an open report, failing when it drew none.
func reportWindowOrFail(t *testing.T, m Model, r reportKind) reportWindow {
	t.Helper()
	win, ok := m.reportWindow(r)
	if !ok {
		t.Fatal("the open report reports no window")
	}
	return win
}

// TestFollowingReportsTrackTheTail is the behaviour the transcript has and these panes lacked: a pane
// left at the end of its rows keeps showing the end as rows arrive under it, one scrolled up off the
// end holds the window the reader put it on, and scrolling back down onto the last full window puts
// it back on the tail. The invariant is TOTAL in both directions — following is exactly "the window
// is at the tail" — so no path can leave the flag saying one thing while the window does another.
func TestFollowingReportsTrackTheTail(t *testing.T) {
	for _, tc := range followCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.model
			m.reportState(tc.kind).follow = true
			win := reportWindowOrFail(t, m, tc.kind)
			if win.end != win.total || win.start == 0 {
				t.Fatalf("precondition: window [%d,%d) of %d rows — a following pane must open at the tail of a list that overflows it",
					win.start, win.end, win.total)
			}

			t.Run("rows arriving under it are shown", func(t *testing.T) {
				grown := reportWindowOrFail(t, tc.grow(t, m), tc.kind)
				if grown.total <= win.total {
					t.Fatalf("the list did not grow: %d rows, was %d", grown.total, win.total)
				}
				if grown.end != grown.total || grown.start <= win.start {
					t.Errorf("window [%d,%d) of %d rows after the growth, want the last full window of the longer list",
						grown.start, grown.end, grown.total)
				}
			})

			t.Run("scrolling up off the end holds the window across the same growth", func(t *testing.T) {
				up := pressReport(t, m, tc.kind, keyUp())
				if up.reportState(tc.kind).follow {
					t.Fatal("↑ off the end left the pane following: the reader scrolled away from the tail")
				}
				held := reportWindowOrFail(t, tc.grow(t, up), tc.kind)
				if held.start != win.start-1 {
					t.Errorf("window starts at %d after the growth, want it held at the row the reader scrolled to (%d)",
						held.start, win.start-1)
				}
			})

			t.Run("scrolling back onto the last full window re-arms it", func(t *testing.T) {
				back := pressReport(t, pressReport(t, m, tc.kind, keyUp()), tc.kind, keyDown())
				if !back.reportState(tc.kind).follow {
					t.Fatal("scrolling back onto the last full window did not re-arm the follow")
				}
				grown := reportWindowOrFail(t, tc.grow(t, back), tc.kind)
				if grown.end != grown.total {
					t.Errorf("window [%d,%d) of %d rows, want the re-armed pane back on the tail",
						grown.start, grown.end, grown.total)
				}
			})
		})
	}
}

// wheelReport rolls one notch with the pointer over the named report and returns the model it left,
// failing when the pane did not claim the notch at all.
func wheelReport(t *testing.T, m Model, r reportKind, button tea.MouseButton) Model {
	t.Helper()
	y0, h, ok := m.reportPaneRect(r)
	if !ok {
		t.Fatal("the report is not on the frame")
	}
	next, handled := m.reportWheel(r, tea.MouseWheelMsg{X: 10, Y: y0 + h/2, Button: button})
	if !handled {
		t.Fatal("the report did not claim a notch over its own box")
	}
	return next
}

// TestTheWheelDetachesAndReArmsTheFollow pins the wheel at both ends of the same invariant the keys
// answer to — and the notch that fires NEITHER of its two guarded writes. Rolling down over a pane
// already at the tail moves nothing, and a follow re-derived only inside those writes would leave a
// pane detached by a wheel-up, whose rows then dropped past their cap onto the tail, with nothing
// left to re-arm it.
func TestTheWheelDetachesAndReArmsTheFollow(t *testing.T) {
	for _, tc := range followCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.model
			m.reportState(tc.kind).follow = true
			win := reportWindowOrFail(t, m, tc.kind)

			up := wheelReport(t, m, tc.kind, tea.MouseWheelUp)
			if up.reportState(tc.kind).follow {
				t.Fatal("a notch up left the pane following: the reader rolled away from the tail")
			}
			held := reportWindowOrFail(t, tc.grow(t, up), tc.kind)
			if held.start != win.start-1 {
				t.Errorf("window starts at %d after the growth, want it held at the row the wheel left it on (%d)",
					held.start, win.start-1)
			}

			back := wheelReport(t, up, tc.kind, tea.MouseWheelDown)
			if !back.reportState(tc.kind).follow {
				t.Fatal("a notch back onto the last full window did not re-arm the follow")
			}
			if grown := reportWindowOrFail(t, tc.grow(t, back), tc.kind); grown.end != grown.total {
				t.Errorf("window [%d,%d) of %d rows, want the re-armed pane back on the tail",
					grown.start, grown.end, grown.total)
			}

			t.Run("a notch that moves nothing still answers for where the pane sits", func(t *testing.T) {
				// The pane's window is at the tail while the flag says otherwise — where a wheel-up
				// detaches a pane whose rows then drop past their cap leaves it. Rolling down fires
				// neither guarded write, and the follow must come back on all the same.
				stuck := m
				stuck.reportState(tc.kind).top = win.start
				stuck.reportState(tc.kind).follow = false
				if got := reportWindowOrFail(t, stuck, tc.kind); got.end != got.total {
					t.Fatalf("precondition: window [%d,%d) of %d rows — the detached pane must sit at the tail",
						got.start, got.end, got.total)
				}
				rolled := wheelReport(t, stuck, tc.kind, tea.MouseWheelDown)
				if !rolled.reportState(tc.kind).follow {
					t.Error("a notch down over a pane already at the tail left it detached with no way back")
				}
				if got := rolled.reportState(tc.kind).top; got != win.start {
					t.Errorf("top = %d after a notch that had nowhere to go, want it left at %d", got, win.start)
				}
			})
		})
	}
}

// TestTheUsageReportDoesNotFollowItsTail is the kind gate, and the whole of what keeps this item off
// the third report: /usage's rows GROW too — a delegate row per run that reports a count — but the
// ratified scope of the follow is the two panes a reader watches arrive, so this one keeps the
// clamp-only scroll it has always had. Both halves are gated: no key or notch ARMS the follow here,
// and a flag set behind the module's back is not HONOURED either.
func TestTheUsageReportDoesNotFollowItsTail(t *testing.T) {
	if usageReport.follows() {
		t.Fatal("/usage follows its tail — the ratified scope is /inspect and /thinking alone")
	}
	m := usageReportModel(t, 40)

	down := pressReport(t, m, usageReport, keyDown())
	if down.usagePane.follow {
		t.Error("↓ armed the follow on /usage")
	}
	end := down
	for range 3 {
		end = pressReport(t, end, usageReport, keyPgDown())
	}
	if end.usagePane.follow {
		t.Error("paging to the end armed the follow on /usage")
	}
	if wheeled := wheelReport(t, down, usageReport, tea.MouseWheelDown); wheeled.usagePane.follow {
		t.Error("a wheel notch armed the follow on /usage")
	}

	grown := delegate(t, down, "late", "late survey", childTotals, 0)
	grown.layout()
	if got := reportWindowOrFail(t, grown, usageReport); got.start != down.usagePane.top {
		t.Errorf("window starts at %d after a delegate row arrived, want it left at %d where the reader put it",
			got.start, down.usagePane.top)
	}

	forced := m
	forced.usagePane.follow = true
	if got := reportWindowOrFail(t, forced, usageReport); got.start != 0 {
		t.Errorf("window starts at %d with the follow set by hand, want the clamped top (0) — /usage does not honour it",
			got.start)
	}
}
