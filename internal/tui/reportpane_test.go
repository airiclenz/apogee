package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
