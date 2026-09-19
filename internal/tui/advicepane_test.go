package tui

import (
	"reflect"
	"strconv"
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
		{run: runRef{}, turn: 1, reaction: "style-check", origin: domain.OriginUser, moment: domain.MomentPostToolResult, detail: "prefer[31m tabs"},
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
	spec, seated := m.adviceSpec()
	if !seated {
		t.Fatal("the frame seated no /advice pane")
	}
	if last := spec.rows[len(spec.rows)-1][0]; last != "the newest advice" {
		t.Errorf("the last row is %q, want the newest firing's detail", last)
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
