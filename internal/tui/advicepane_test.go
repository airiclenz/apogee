package tui

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/airiclenz/apogee/internal/domain"
)

// advisedAt is one advise firing as the engine books it: a ReactionFiredEvent stamped with a run
// identity and a Turn, carrying the action label, the reaction that fired, whose it was and the
// Detail it carried. Every firing the tests fold goes through it so the shape is stated once.
func advisedAt(run runRef, turn int, action, reaction string, origin domain.Origin, detail string) domain.ReactionFiredEvent {
	return domain.ReactionFiredEvent{
		EventBase: eventBaseAt(run, turn),
		Reaction:  reaction,
		Origin:    origin,
		Moment:    domain.MomentPostToolResult,
		Action:    action,
		Detail:    detail,
	}
}

// advicePaneModel folds `records` user advise firings on the main run, one per Turn, and opens the
// pane at the top, not following — the state every scroll claim below needs a full window under.
func advicePaneModel(t *testing.T, records int) Model {
	t.Helper()
	m := newTestModel(t)
	for i := range records {
		m = m.foldEvent(advisedAt(runRef{}, i, adviceActionAdvise, "style-check", domain.OriginUser, "turn "+strconv.Itoa(i)+" advice"))
	}
	m.advicePane = reportPane{open: true}
	m.layout()
	return m
}

// TestAdviceBoardFoldsAdviseFiringsInOrder pins the board's whole intake: a firing booked under
// "advise" (a user entry's capped fenced text) and one under "notice" (the context-fill notice's
// rung) land on the board in arrival order with their Detail escape-stripped at the seam (the ESC
// dropped and the bytes after it kept — stripEscapes's own contract), a firing under any other
// action — a Floor guard's retry, a bare "fired" — and every non-firing Event leave it untouched,
// and past maxAdviceRecords the OLDEST firing is the one dropped, so a session whose advise entry
// fires on every tool call keeps its newest firings rather than its first.
func TestAdviceBoardFoldsAdviseFiringsInOrder(t *testing.T) {
	t.Parallel()

	child := runRef{depth: 1, spawn: "call-a"}
	m := newTestModel(t)
	m = m.foldEvent(advisedAt(runRef{}, 1, adviceActionAdvise, "style-check", domain.OriginUser, "prefer\x1b[31m tabs"))
	m = m.foldEvent(advisedAt(runRef{}, 1, "retry", "tool-call-repair", domain.OriginEngine, ""))
	m = m.foldEvent(advisedAt(child, 2, adviceActionNotice, "context-fill-notice", domain.OriginEngine, "rung 1 (50%)"))
	m = m.foldEvent(advisedAt(runRef{}, 2, "fired", "audit-log", domain.OriginUser, "seen"))
	m = m.foldEvent(domain.MessageEvent{EventBase: eventBaseAt(runRef{}, 2)})

	want := []adviceRecord{
		{run: runRef{}, turn: 1, reaction: "style-check", origin: domain.OriginUser, moment: domain.MomentPostToolResult, detail: "prefer tabs"},
		{run: child, turn: 2, reaction: "context-fill-notice", origin: domain.OriginEngine, moment: domain.MomentPostToolResult, detail: "rung 1 (50%)"},
	}
	if !reflect.DeepEqual(m.advice, want) {
		t.Fatalf("board after the folds:\n got %+v\nwant %+v", m.advice, want)
	}

	// The cap's bite: the board holds exactly maxAdviceRecords once more than that have fired, the
	// dropped records are the OLDEST, and the newest firing is always kept.
	const over = 5
	m = newTestModel(t)
	for i := range maxAdviceRecords + over {
		m = m.foldEvent(advisedAt(runRef{}, i, adviceActionAdvise, "style-check", domain.OriginUser, "advice "+strconv.Itoa(i)))
	}
	if len(m.advice) != maxAdviceRecords {
		t.Fatalf("board holds %d records past the cap, want %d", len(m.advice), maxAdviceRecords)
	}
	if first := m.advice[0].turn; first != over {
		t.Errorf("the oldest kept record is turn %d, want %d — the cap dropped the wrong end", first, over)
	}
	if last := m.advice[len(m.advice)-1].turn; last != maxAdviceRecords+over-1 {
		t.Errorf("the newest kept record is turn %d, want %d", last, maxAdviceRecords+over-1)
	}
}

// TestAdvicePaneGroupsRowsByRunAndTurn is the pane's whole composition: one heading per run of
// firings sharing a run and a Turn — `turn N`, or `<run label> · turn N` for a delegate's, spelled
// as the /thinking pane spells it — then per firing the plain `<reaction> (<origin> origin) @
// <moment>` row and its Detail, wrapped, a firing with no Detail being its naming row alone. The
// heading and the firing row are two rows of two kinds: the What's `turn N · <reaction> …` reading
// is that PAIR, never one heading row.
func TestAdvicePaneGroupsRowsByRunAndTurn(t *testing.T) {
	t.Parallel()

	const column = 40
	child := runRef{depth: 1, spawn: "call-a"}

	m := newTestModel(t)
	if rows, kinds := m.adviceRows(column); !reflect.DeepEqual(rows, []popupRow{{adviceEmptyRow}}) || !reflect.DeepEqual(kinds, []popupRowKind{popupRowPlain}) {
		t.Fatalf("an empty board composes %v / %v, want the one empty-state row", rows, kinds)
	}

	m = m.foldEvent(advisedAt(runRef{}, 1, adviceActionAdvise, "style-check", domain.OriginUser, "prefer tabs\nnever spaces"))
	m = m.foldEvent(advisedAt(runRef{}, 1, adviceActionNotice, "context-fill-notice", domain.OriginEngine, "rung 1 (50%)"))
	m = m.foldEvent(advisedAt(child, 1, adviceActionAdvise, "style-check", domain.OriginUser, "a line of advice long enough that the pane has to wrap it"))
	m = m.foldEvent(advisedAt(runRef{}, 2, adviceActionAdvise, "style-check", domain.OriginUser, ""))

	rows, kinds := m.adviceRows(column)
	wantRows := []string{
		"turn 1",
		"style-check (user origin) @ post-tool-result",
		"prefer tabs",
		"never spaces",
		"context-fill-notice (engine origin) @ post-tool-result",
		"rung 1 (50%)",
		usageAgentFallback + " · turn 1",
		"style-check (user origin) @ post-tool-result",
		"a line of advice long enough that the",
		"  pane has to wrap it",
		"turn 2",
		"style-check (user origin) @ post-tool-result",
	}
	wantKinds := []popupRowKind{
		popupRowHeading, popupRowPlain, popupRowPlain, popupRowPlain, popupRowPlain, popupRowPlain,
		popupRowHeading, popupRowPlain, popupRowPlain, popupRowPlain,
		popupRowHeading, popupRowPlain,
	}
	got := make([]string, len(rows))
	for i, row := range rows {
		got[i] = row[0]
	}
	if !reflect.DeepEqual(got, wantRows) {
		t.Errorf("rows:\n got %q\nwant %q", got, wantRows)
	}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Errorf("kinds:\n got %v\nwant %v", kinds, wantKinds)
	}

	// The heading is /thinking's spelling exactly, for both run shapes: a reader moving between the
	// two panes reads the same owner off the same words.
	for _, run := range []runRef{{}, child} {
		if got, want := m.adviceHeading(run, 3), m.thinkingHeading(thinkingRecord{run: run, turn: 3}); got != want {
			t.Errorf("advice heading for %+v is %q, want the thinking heading %q", run, got, want)
		}
	}
}

// TestAdvicePaneFollowsTheNewestFiring pins the pane's follow under its own name: opened on the
// tail and following, a firing that lands while it is up is on the window the next frame draws —
// the last full window ends at the last row — rather than below a frozen one.
func TestAdvicePaneFollowsTheNewestFiring(t *testing.T) {
	t.Parallel()

	m := advicePaneModel(t, 12)
	rows, _ := m.adviceRows(m.thinkingWrapColumn())
	m.advicePane = reportPane{open: true, top: len(rows), follow: true}
	m.layout()

	before := reportWindowOrFail(t, m, adviceReport)
	if before.end != before.total {
		t.Fatalf("an opened pane shows rows [%d, %d) of %d, want the tail", before.start, before.end, before.total)
	}

	m = m.foldEvent(advisedAt(runRef{}, 99, adviceActionAdvise, "style-check", domain.OriginUser, "the newest advice"))
	after := reportWindowOrFail(t, m, adviceReport)
	if after.total <= before.total {
		t.Fatalf("the firing added no rows: %d before, %d after", before.total, after.total)
	}
	if after.end != after.total {
		t.Errorf("after a firing the pane shows rows [%d, %d) of %d, want the tail still", after.start, after.end, after.total)
	}
	spec, seated := m.reportSpec(adviceReport, m.adviceContent())
	if !seated {
		t.Fatal("the frame seated no /advice pane")
	}
	if last := spec.rows[len(spec.rows)-1][0]; last != "the newest advice" {
		t.Errorf("the last row is %q, want the newest firing's detail", last)
	}
}

// TestDismissingTheAdvicePaneDropsTheScroll pins the other half of where the verb lands: closing the
// pane takes the scroll with it, so the next /advice opens on the newest firing again rather than on
// the window the last reading was left at. Both ways of closing spend the one body (dismissReport),
// so proving it here proves it for esc and for a click outside alike.
func TestDismissingTheAdvicePaneDropsTheScroll(t *testing.T) {
	t.Parallel()

	m := advicePaneModel(t, 40)
	m.advicePane.top = 7

	closed := m.dismissReport(adviceReport)

	if closed.advicePane.open {
		t.Error("the dismissed pane is still on the frame")
	}
	if closed.advicePane.top != 0 {
		t.Errorf("top = %d after dismissal, want 0 — the next /advice opens on the newest firing",
			closed.advicePane.top)
	}
}

// TestAdviceCommand pins the verb's whole contract: /advice opens the pane on the newest firing,
// following, and drives no worker; a second /advice on the open pane does not toggle it shut but
// RE-OPENS it — the follow a reader had detached by scrolling up is re-armed and the window is the
// tail again, exactly as /thinking's verb behaves; and esc is what closes it. The verb is always
// "show me the newest", never "hide it".
func TestAdviceCommand(t *testing.T) {
	t.Parallel()

	t.Run("opens on the newest firing and drives no worker", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m = m.foldEvent(advisedAt(runRef{}, 1, adviceActionAdvise, "style-check", domain.OriginUser, "keep the diff small"))
		m.input.SetValue("/advice")
		m, cmd := stepCmd(t, m, keyEnter())

		if !m.advicePane.open || !m.advicePane.follow {
			t.Fatalf("/advice pane = %+v; want it open and following", m.advicePane)
		}
		if m.state != stateIdle || cmd != nil {
			t.Errorf("state = %v, cmd = %v; /advice drives no worker", m.state, cmd)
		}
		if !m.openPanes().has(paneAdvice) {
			t.Error("the open pane is not in the frame's pane set — it would be drawn on rows nothing budgeted")
		}
		painted := strip(m.frameOverlays().advice)
		for _, want := range []string{adviceTitle, "style-check (user origin) @ post-tool-result", "keep the diff small"} {
			if !strings.Contains(painted, want) {
				t.Errorf("the frame does not stack the pane it opened — %q missing:\n%s", want, painted)
			}
		}
	})

	t.Run("a second /advice re-arms the follow instead of toggling the pane shut", func(t *testing.T) {
		t.Parallel()

		m := advicePaneModel(t, 12)
		m.input.SetValue("/advice")
		m = step(t, m, keyEnter())
		m = step(t, m, keyUp())
		if m.advicePane.follow {
			t.Fatal("scrolling up off the end did not detach the follow — the premise of the re-arm claim")
		}

		m.input.SetValue("/advice")
		m = step(t, m, keyEnter())
		if !m.advicePane.open {
			t.Fatal("a second /advice closed the pane; the verb never toggles")
		}
		if !m.advicePane.follow {
			t.Error("a second /advice did not re-arm the follow")
		}
		window := reportWindowOrFail(t, m, adviceReport)
		if window.end != window.total {
			t.Errorf("after a second /advice the pane shows rows [%d, %d) of %d, want the tail", window.start, window.end, window.total)
		}
	})

	t.Run("esc closes it", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m.input.SetValue("/advice")
		m = step(t, m, keyEnter())
		if !m.advicePane.open {
			t.Fatal("/advice did not open the pane")
		}
		m = step(t, m, keyEsc())
		if m.advicePane.open {
			t.Error("esc did not close the pane")
		}
		if m.openPanes().has(paneAdvice) {
			t.Error("the closed pane is still in the frame's pane set")
		}
	})
}

// TestAdviceOpensOnTheNewestFiring pins the verb's landing: with more firings than the pane can
// seat, /advice opens on the LAST full window — the newest firing's Detail is the last row drawn
// and the first firing is off the top — so a long session's pane says something on arrival instead
// of asking for a hundred page-downs.
func TestAdviceOpensOnTheNewestFiring(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	for i := range 40 {
		m = m.foldEvent(advisedAt(runRef{}, i, adviceActionAdvise, "style-check", domain.OriginUser, "turn "+strconv.Itoa(i)+" advice"))
	}
	m.input.SetValue("/advice")
	m = step(t, m, keyEnter())

	window := reportWindowOrFail(t, m, adviceReport)
	if window.end != window.total {
		t.Fatalf("/advice opened on rows [%d, %d) of %d, want the tail", window.start, window.end, window.total)
	}
	if window.start == 0 {
		t.Fatalf("the whole board fits in %d rows — the test premise needs more firings than the pane seats", window.total)
	}
	spec, seated := m.reportSpec(adviceReport, m.adviceContent())
	if !seated {
		t.Fatal("the frame seated no /advice pane")
	}
	if last := spec.rows[len(spec.rows)-1][0]; last != "turn 39 advice" {
		t.Errorf("the last drawn row is %q, want the newest firing's detail", last)
	}
	painted := strip(m.frameOverlays().advice)
	if strings.Contains(painted, "turn 0 advice") {
		t.Errorf("the oldest firing is drawn on an opened pane that should show the tail:\n%s", painted)
	}
}

// TestFramePaneSetHoldsEveryPane is the runtime half of the guard beside framePaneSet: every
// declared framePane has a bit of its own inside the set's width, so a pane past the width could
// not be silently shifted off the end, declared and never seated. The compile-time array guard in
// model.go fails the build first; this states the same fact where a reader looks for it.
func TestFramePaneSetHoldsEveryPane(t *testing.T) {
	t.Parallel()

	if bits := int(unsafe.Sizeof(framePaneSet(0))) * 8; bits < int(paneKinds) {
		t.Fatalf("framePaneSet holds %d bits for %d panes", bits, paneKinds)
	}
	for p := framePane(0); p < paneKinds; p++ {
		var s framePaneSet
		s = s.with(p)
		for q := framePane(0); q < paneKinds; q++ {
			if s.has(q) != (p == q) {
				t.Errorf("with(%d).has(%d) = %t", p, q, s.has(q))
			}
		}
	}
}
