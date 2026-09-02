package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// thinkingBoardWith builds a board out of the given committed and in-flight records, in the order
// given — the shape a run of folds would have left, stated directly so a rendering test says what
// it renders rather than replaying an Event stream to get there.
func thinkingBoardWith(done, live []thinkingRecord) thinkingBoard {
	return thinkingBoard{done: done, live: live}
}

// TestThinkingRows is the pane's whole composition: which records a frame speaks for, the order it
// puts them in, the headings it spells over them, and the plain rows it makes of their text.
func TestThinkingRows(t *testing.T) {
	const column = 40

	main1 := thinkingRecord{run: runRef{}, turn: 1, text: "first thought"}
	main2 := thinkingRecord{run: runRef{}, turn: 2, text: "second thought"}
	childA := thinkingRecord{run: runRef{depth: 1, spawn: "call-a"}, turn: 1, text: "child a thinks"}
	childB := thinkingRecord{run: runRef{depth: 1, spawn: "call-b"}, turn: 1, text: "child b thinks"}

	cases := []struct {
		name      string
		board     thinkingBoard
		viewStack []runView
		wantRows  []string
		wantKinds []popupRowKind
	}{
		{
			name:      "an empty board is the one empty-state row",
			board:     thinkingBoard{},
			wantRows:  []string{thinkingEmptyRow},
			wantKinds: []popupRowKind{popupRowPlain},
		},
		{
			name:      "one main-agent record is a turn heading and its text",
			board:     thinkingBoardWith([]thinkingRecord{main1}, nil),
			wantRows:  []string{"turn 1", "first thought"},
			wantKinds: []popupRowKind{popupRowHeading, popupRowPlain},
		},
		{
			name:      "committed records paint oldest first",
			board:     thinkingBoardWith([]thinkingRecord{main1, main2}, nil),
			wantRows:  []string{"turn 1", "first thought", "turn 2", "second thought"},
			wantKinds: []popupRowKind{popupRowHeading, popupRowPlain, popupRowHeading, popupRowPlain},
		},
		{
			name:      "at the top level a fan-out's children are filtered out",
			board:     thinkingBoardWith([]thinkingRecord{main1, childA, childB}, nil),
			wantRows:  []string{"turn 1", "first thought"},
			wantKinds: []popupRowKind{popupRowHeading, popupRowPlain},
		},
		{
			name:      "a run view shows that run alone, under its own heading",
			board:     thinkingBoardWith([]thinkingRecord{main1, childA, childB}, nil),
			viewStack: []runView{{ref: runRef{depth: 1, spawn: "call-a"}}},
			wantRows:  []string{usageAgentFallback + " · turn 1", "child a thinks"},
			wantKinds: []popupRowKind{popupRowHeading, popupRowPlain},
		},
		{
			name:      "a scope with no records is the same empty-state row",
			board:     thinkingBoardWith([]thinkingRecord{main1}, nil),
			viewStack: []runView{{ref: runRef{depth: 1, spawn: "call-a"}}},
			wantRows:  []string{thinkingEmptyRow},
			wantKinds: []popupRowKind{popupRowPlain},
		},
		{
			name: "the live record lands at the tail, after every committed one",
			board: thinkingBoardWith(
				[]thinkingRecord{main1, main2},
				[]thinkingRecord{{run: runRef{}, turn: 3, text: "still going"}},
			),
			wantRows: []string{
				"turn 1", "first thought",
				"turn 2", "second thought",
				"turn 3", "still going",
			},
			wantKinds: []popupRowKind{
				popupRowHeading, popupRowPlain,
				popupRowHeading, popupRowPlain,
				popupRowHeading, popupRowPlain,
			},
		},
		{
			name: "a sibling's in-flight record is out of scope at the top level",
			board: thinkingBoardWith(
				[]thinkingRecord{main1},
				[]thinkingRecord{childA, {run: runRef{}, turn: 2, text: "mine"}},
			),
			wantRows:  []string{"turn 1", "first thought", "turn 2", "mine"},
			wantKinds: []popupRowKind{popupRowHeading, popupRowPlain, popupRowHeading, popupRowPlain},
		},
		{
			name: "a line the model wrote starts flush; only a wrap is indented",
			board: thinkingBoardWith([]thinkingRecord{{
				run:  runRef{},
				turn: 7,
				text: "alpha beta gamma delta epsilon zeta eta theta\nsecond paragraph",
			}}, nil),
			wantRows: []string{
				"turn 7",
				"alpha beta gamma delta epsilon zeta eta",
				"  theta",
				"second paragraph",
			},
			wantKinds: []popupRowKind{popupRowHeading, popupRowPlain, popupRowPlain, popupRowPlain},
		},
		{
			name: "a blank line the model wrote stays a blank row",
			board: thinkingBoardWith([]thinkingRecord{{
				run: runRef{}, turn: 4, text: "one\n\ntwo",
			}}, nil),
			wantRows:  []string{"turn 4", "one", "", "two"},
			wantKinds: []popupRowKind{popupRowHeading, popupRowPlain, popupRowPlain, popupRowPlain},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			m.thinking = tc.board
			m.viewStack = tc.viewStack

			rows, kinds := m.thinkingRows(column)
			got := make([]string, len(rows))
			for i, row := range rows {
				if len(row) != 1 {
					t.Fatalf("row %d has %d columns, want a single-cell row: %q", i, len(row), row)
				}
				got[i] = row[0]
			}
			if len(got) != len(tc.wantRows) {
				t.Fatalf("rows = %q, want %q", got, tc.wantRows)
			}
			for i := range got {
				if got[i] != tc.wantRows[i] {
					t.Errorf("row %d = %q, want %q", i, got[i], tc.wantRows[i])
				}
			}
			if len(kinds) != len(tc.wantKinds) {
				t.Fatalf("kinds = %v, want %v", kinds, tc.wantKinds)
			}
			for i := range kinds {
				if kinds[i] != tc.wantKinds[i] {
					t.Errorf("kind %d = %v, want %v", i, kinds[i], tc.wantKinds[i])
				}
			}
		})
	}
}

// TestThinkingRowsCarryNoClutter is the pane's reason for existing, stated as an assertion: the
// rows are the model's own text and nothing else. /inspect's readable rendering dresses the same
// reasoning with a kind-naming prefix; this pane must not, and a rendering tweak that reintroduced
// one — a prefix, a JSON fragment, a per-record elision, turn metadata inside the body — would
// have to change this test by name.
func TestThinkingRowsCarryNoClutter(t *testing.T) {
	m := newTestModel(t)
	m.thinking = thinkingBoardWith([]thinkingRecord{{
		run: runRef{}, turn: 2, text: "weigh the options",
	}}, nil)

	rows, kinds := m.thinkingRows(60)
	if len(rows) != 2 {
		t.Fatalf("rows = %q, want exactly a heading and one body row", rows)
	}
	if rows[0][0] != "turn 2" || kinds[0] != popupRowHeading {
		t.Errorf("heading row = %q (kind %v), want %q as a heading", rows[0][0], kinds[0], "turn 2")
	}
	if rows[1][0] != "weigh the options" {
		t.Errorf("body row = %q, want the model's text verbatim", rows[1][0])
	}
	for _, clutter := range []string{readableThinkingPrefix, readableTextPrefix, readableToolCallPrefix, "{", "json"} {
		if strings.Contains(rows[1][0], strings.TrimSpace(clutter)) {
			t.Errorf("body row %q carries %q — this pane is plain text", rows[1][0], clutter)
		}
	}
}

// TestThinkingContentNamesItself pins what the pane tells the shared report module: its title (the
// viewed run's name folded in under a run view), the keys it spells, and the absence of a ctrl+r
// key it does not answer.
func TestThinkingContentNamesItself(t *testing.T) {
	m := newTestModel(t)
	m.thinking = thinkingBoardWith([]thinkingRecord{{run: runRef{}, turn: 1, text: "hm"}}, nil)

	c := m.thinkingContent()
	if c.title != thinkingTitle {
		t.Errorf("title = %q, want %q", c.title, thinkingTitle)
	}
	if c.hint != thinkingHint {
		t.Errorf("hint = %q, want %q", c.hint, thinkingHint)
	}
	if strings.Contains(c.hint, "ctrl+r") {
		t.Errorf("hint = %q names a key this pane does not answer", c.hint)
	}
	if c.rowCap != maxInspectorRows {
		t.Errorf("rowCap = %d, want %d", c.rowCap, maxInspectorRows)
	}

	m.viewStack = []runView{{ref: runRef{depth: 1, spawn: "call-a"}}}
	m.thinking = thinkingBoardWith([]thinkingRecord{{
		run: runRef{depth: 1, spawn: "call-a"}, turn: 1, text: "hm",
	}}, nil)
	if want := thinkingTitle + " — " + usageAgentFallback; m.thinkingContent().title != want {
		t.Errorf("scoped title = %q, want %q", m.thinkingContent().title, want)
	}
}

// TestThinkingWrapColumnFollowsTheWindow pins the derivation the LOSS test below depends on: the
// column is the pane's inner width less the marker column and the overflow bar's reserved one, and
// it never falls under the floor however narrow the window gets.
func TestThinkingWrapColumnFollowsTheWindow(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		m := newTestModel(t)
		m.width = width
		want := popupInnerWidth(m.th, width) - popupRowIndent - scrollbarWidth
		if got := m.thinkingWrapColumn(); got != want {
			t.Errorf("width %d: wrap column = %d, want %d", width, got, want)
		}
	}
	m := newTestModel(t)
	m.width = 4
	if got := m.thinkingWrapColumn(); got != minThinkingWrapColumn {
		t.Errorf("width 4: wrap column = %d, want the floor %d", got, minThinkingWrapColumn)
	}
}

// TestThinkingPaneLosesNoText is why the wrap column is derived rather than constant. Report rows
// are TRUNCATED to the pane's inner width and never re-wrapped (popup.go), and this pane has no raw
// toggle and no horizontal scroll, so a row composed wider than the pane is text the reader can
// never get back. The assertion is a LOSS test and not an exact-row-text one, because the column is
// derived: every rune of a canned record's text must appear, in order, in what the pane PAINTS —
// at a narrow terminal as well as a wide one. A fixed 96-column wrap fails it at width 80.
func TestThinkingPaneLosesNoText(t *testing.T) {
	text := strings.Join([]string{
		"The first line of reasoning runs well past a hundred columns so that any fixed wrap column has to cut it somewhere.",
		"A second line, shorter, but still comfortably wider than an eighty-column terminal can seat in one row.",
	}, "\n")

	for _, width := range []int{80, 200} {
		m := newTestModel(t)
		m.width = width
		m.thinking = thinkingBoardWith([]thinkingRecord{{run: runRef{}, turn: 1, text: text}}, nil)

		painted := paintThinkingPane(m)
		if rest, ok := containsRunesInOrder(painted, text); !ok {
			t.Errorf("width %d: the painted pane drops the record's text at %q", width, rest)
		}
	}
}

// paintThinkingPane paints the pane's rows through the popup module at the model's own width — the
// same renderPopup path [Model.renderReport] takes, with every row granted a line so the assertion
// is about TRUNCATION and not about the scroll window. The spec is composed here rather than through
// [Model.reportSpec] so that every row is seated whatever the window grants.
func paintThinkingPane(m Model) string {
	c := m.thinkingContent()
	return renderPopup(m.th, popupSpec{
		title:       c.title,
		body:        c.body,
		maxBodyRows: -1,
		rows:        c.rows,
		rowKinds:    c.kinds,
		selected:    -1,
		hint:        c.hint,
		maxRows:     len(c.rows),
		scrollbar:   m.popupScrollbarOn(),
	}, m.width)
}

// containsRunesInOrder reports whether every rune of want appears in painted in order, ignoring the
// whitespace the wrap and the pane's own padding introduce. It returns the unmatched remainder so a
// failure names where the text was lost rather than only that it was.
func containsRunesInOrder(painted, want string) (rest string, ok bool) {
	i := 0
	runes := []rune(strings.Join(strings.Fields(want), " "))
	for _, r := range strings.Join(strings.Fields(painted), " ") {
		if i < len(runes) && r == runes[i] {
			i++
		}
	}
	if i == len(runes) {
		return "", true
	}
	return string(runes[i:]), false
}

// thinkingPaneModel opens the /thinking pane over more records than it can seat rows for, which is
// the state the shared scroll and hit-test drivers are about. The scroll starts at the TOP rather
// than where the verb opens it ([Model.runThinkingCommand] seats the last window), because a scroll
// test needs rows below the window to move on to.
func thinkingPaneModel(t *testing.T, records int) Model {
	t.Helper()
	m := newTestModel(t)
	for i := range records {
		m = m.foldEvent(reasoningAt(runRef{}, i, "turn "+strconv.Itoa(i)+" reasoning"))
		m = m.foldEvent(domain.MessageEvent{EventBase: eventBaseAt(runRef{}, i)})
	}
	m.thinkingPane = reportPane{open: true}
	m.layout()
	return m
}

// TestThinkingCommand drives the verb the way a human types it — the exact announced name, then ⏎ —
// and pins the three things opening it settles: the pane joins the frame's own pane set (so it is
// drawn on rows something budgeted), it is scoped as the VIEW is, and it stays a report rather than
// a modal — a printable key still reaches the box behind it.
func TestThinkingCommand(t *testing.T) {
	t.Run("the verb opens the pane and drives no worker", func(t *testing.T) {
		m := newTestModel(t)
		m = m.foldEvent(reasoningAt(runRef{}, 1, "weigh the options"))
		m.input.SetValue("/thinking")
		m, cmd := stepCmd(t, m, keyEnter())

		if !m.thinkingPane.open {
			t.Fatal("/thinking did not open the pane")
		}
		if m.state != stateIdle || cmd != nil {
			t.Errorf("state = %v, cmd = %v; /thinking drives no worker", m.state, cmd)
		}
		if !m.openPanes().has(paneThinking) {
			t.Error("the open pane is not in the frame's pane set — it would be drawn on rows nothing budgeted")
		}
		painted := strip(m.frameOverlays().thinking)
		if !strings.Contains(painted, thinkingTitle) || !strings.Contains(painted, "weigh the options") {
			t.Errorf("the frame does not stack the pane it opened:\n%s", painted)
		}
	})

	t.Run("inside a run view it is the viewed run's thinking alone", func(t *testing.T) {
		child := runRef{depth: 1, spawn: "call-a"}
		m := newTestModel(t)
		m = m.foldEvent(reasoningAt(runRef{}, 1, "the orchestrator's own thought"))
		m = m.foldEvent(reasoningAt(child, 1, "the delegate's own thought"))
		m.viewStack = []runView{{ref: child}}
		m.input.SetValue("/thinking")
		m = step(t, m, keyEnter())

		painted := strip(m.frameOverlays().thinking)
		if want := thinkingTitle + " — " + usageAgentFallback; !strings.Contains(painted, want) {
			t.Errorf("the box is not titled %q:\n%s", want, painted)
		}
		if !strings.Contains(painted, "the delegate's own thought") {
			t.Errorf("the viewed run's thinking is not in the pane:\n%s", painted)
		}
		if strings.Contains(painted, "the orchestrator's own thought") {
			t.Errorf("the pane shows thinking from outside the viewed run:\n%s", painted)
		}
	})

	t.Run("a printable key still reaches the box behind it", func(t *testing.T) {
		m := thinkingPaneModel(t, 4)

		m = step(t, m, keyRune('x'))

		if !m.thinkingPane.open {
			t.Error("a printable key closed the pane; it is a report, not a modal")
		}
		if got := m.input.Value(); got != "x" {
			t.Errorf("draft = %q, want %q — the box behind a report stays live", got, "x")
		}
	})
}

// TestThinkingOpensOnTheNewestRecord pins where the verb lands: on the LAST full window, so the
// Turn worth reading — the one the human just watched the model work through — is on the screen
// without a page-down per record, and the scroll keys then move from THERE.
func TestThinkingOpensOnTheNewestRecord(t *testing.T) {
	m := thinkingPaneModel(t, 40)
	m.thinkingPane = reportPane{} // reopen through the verb itself, which is what sets the scroll
	next, _ := m.runThinkingCommand()
	m = next.(Model)

	spec, seated := m.thinkingSpec()
	if !seated {
		t.Fatal("the frame seated no pane for a full board")
	}
	if len(spec.rows) <= spec.maxRows {
		t.Fatalf("precondition: %d rows into a window of %d — the board must overflow the pane for a scroll to mean anything",
			len(spec.rows), spec.maxRows)
	}
	if spec.rowTop+spec.maxRows != len(spec.rows) {
		t.Errorf("window [%d,%d) of %d rows, want the last full window", spec.rowTop, spec.rowTop+spec.maxRows, len(spec.rows))
	}
}

// TestDismissingTheThinkingPaneDropsTheScroll pins the other half of where the verb lands: closing
// the pane takes the scroll with it, so the next /thinking opens on the newest record again rather
// than on the window the last reading was left at. Both ways of closing spend dismissThinking's one
// body (dismissReport), so proving it here proves it for esc and for a click outside alike.
func TestDismissingTheThinkingPaneDropsTheScroll(t *testing.T) {
	m := thinkingPaneModel(t, 40)
	m.thinkingPane.top = 7

	closed := m.dismissThinking()

	if closed.thinkingPane.open {
		t.Error("the dismissed pane is still on the frame")
	}
	if closed.thinkingPane.top != 0 {
		t.Errorf("top = %d after dismissal, want 0 — the next /thinking opens on the newest record",
			closed.thinkingPane.top)
	}
}
