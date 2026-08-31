package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/session"
)

// ----------------------------------------------------------------------------
// /usage — the per-agent token report (usage.go)
// ----------------------------------------------------------------------------

// usageModel builds a ready model with the main agent's totals and fill already folded and the
// report open, which is the state every assertion below is about.
func usageModel(t *testing.T, totals usageTotals, used int) Model {
	t.Helper()
	m := newTestModel(t)
	m.usage = totals
	m.ctxUsed = used
	m.usagePane = usagePane{open: true}
	return m
}

// delegate adds a sub-agent run to the transcript with the totals and fill its child reported.
func delegate(t *testing.T, m Model, id, task string, totals usageTotals, used int) Model {
	t.Helper()
	subAgentCall(&m.transcript, id, task, 0)
	head := &m.transcript.entries[len(m.transcript.entries)-1]
	head.usage = totals
	if used > 0 {
		head.ctxUsed, head.ctxLimit = used, m.opts.ContextWindow
	}
	return m
}

var (
	mainTotals  = usageTotals{Calls: 3, PromptTokens: 20000, CompletionTokens: 1500, TotalTokens: 21500}
	childTotals = usageTotals{Calls: 2, PromptTokens: 4000, CompletionTokens: 500, TotalTokens: 4500}
)

// TestUsageRowsReportEveryAgentThatSpent pins what the report is made of: the column header, the
// main agent, each delegate that reported a count — in transcript order — and the session total,
// which appears only where there is more than one agent to add up.
func TestUsageRowsReportEveryAgentThatSpent(t *testing.T) {
	t.Run("the main agent alone gets no session row", func(t *testing.T) {
		rows := usageModel(t, mainTotals, 8192).usageRows()

		if len(rows) != 2 {
			t.Fatalf("rows = %q, want the header and the main agent alone", rows)
		}
		if got, want := rows[0], usageHeaderCells(false); !equalRow(got, want) {
			t.Errorf("first row = %q, want the column header %q", got, want)
		}
		want := popupRow{usageMainLabel, "3", format.Tokens(20000), format.Tokens(1500), format.Tokens(21500), "25%"}
		if !equalRow(rows[1], want) {
			t.Errorf("main row = %q, want %q", rows[1], want)
		}
		// One agent IS the session: a total row here would restate the row above it.
		for _, row := range rows {
			if len(row) > 0 && row[0] == usageSessionLabel {
				t.Errorf("a lone agent drew a session total: %q", row)
			}
		}
	})

	t.Run("delegates come in transcript order and the session row sums them", func(t *testing.T) {
		m := usageModel(t, mainTotals, 8192)
		m = delegate(t, m, "s1", "survey the tests", childTotals, 16384)
		m = delegate(t, m, "s2", "survey the docs", childTotals, 0)
		rows := m.usageRows()

		if len(rows) != 5 {
			t.Fatalf("rows = %q, want header, main, two delegates and the session total", rows)
		}
		for i, want := range []string{"survey the tests", "survey the docs"} {
			if got := rows[2+i]; !strings.Contains(got[0], want) {
				t.Errorf("delegate row %d = %q, want it named %q in transcript order", i, got, want)
			}
			if !strings.HasPrefix(rows[2+i][0], usageIndent) {
				t.Errorf("delegate row %d = %q, want it indented under the main agent", i, rows[2+i])
			}
		}
		// Only the first delegate reported a fill; the second's cell is empty rather than a 0%.
		if got := rows[2][5]; got != "50%" {
			t.Errorf("first delegate fill = %q, want 50%%", got)
		}
		if got := rows[3][5]; got != "" {
			t.Errorf("second delegate fill = %q, want no cell — it reported no fill", got)
		}

		sum := usageSum(usageSum(mainTotals, childTotals), childTotals)
		want := popupRow{usageSessionLabel, "7",
			format.Tokens(sum.PromptTokens), format.Tokens(sum.CompletionTokens), format.Tokens(sum.TotalTokens), ""}
		if !equalRow(rows[4], want) {
			t.Errorf("session row = %q, want %q — the agents' own totals added up", rows[4], want)
		}
	})

	t.Run("a delegate that reported nothing is left out", func(t *testing.T) {
		m := usageModel(t, mainTotals, 8192)
		m = delegate(t, m, "s1", "survey the tests", usageTotals{}, 0)

		if rows := m.usageRows(); len(rows) != 2 {
			t.Errorf("rows = %q, want the header and the main agent — a run with no count is not a spend", rows)
		}
	})

	t.Run("nothing reported at all is no rows", func(t *testing.T) {
		if rows := usageModel(t, usageTotals{}, 0).usageRows(); rows != nil {
			t.Errorf("rows = %q, want none — the pane says so in prose instead", rows)
		}
	})
}

// equalRow compares two rows cell by cell, which is what the assertions above are about.
func equalRow(got, want popupRow) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestUsagePanePaintsItsRowsAndSaysWhenThereAreNone drives the renderer itself: the pane names
// itself, spells its one key, carries the rows composed above — and, with nothing counted, says so
// on a line of prose rather than drawing a header over an empty table.
func TestUsagePanePaintsItsRowsAndSaysWhenThereAreNone(t *testing.T) {
	m := usageModel(t, mainTotals, 8192)
	m = delegate(t, m, "s1", "survey the tests", childTotals, 16384)
	pane := strip(m.renderUsage())

	for _, want := range []string{usageTitle, usageHint, usageMainLabel, usageSessionLabel,
		"survey the tests", usageHeaderCells(false)[1], format.Tokens(21500)} {
		if !strings.Contains(pane, want) {
			t.Errorf("the pane does not show %q:\n%s", want, pane)
		}
	}

	empty := strip(usageModel(t, usageTotals{}, 0).renderUsage())
	if !strings.Contains(empty, usageEmptyBody) {
		t.Errorf("the empty pane does not say why it is empty:\n%s", empty)
	}
	if strings.Contains(empty, usageMainLabel) {
		t.Errorf("the empty pane drew an agent row that reported nothing:\n%s", empty)
	}

	if closed := m.closedUsage().renderUsage(); closed != "" {
		t.Errorf("the closed pane rendered %q, want nothing", closed)
	}
}

// closedUsage is the model with the report dismissed — the renderer's own "" condition.
func (m Model) closedUsage() Model {
	m.usagePane = usagePane{}
	return m
}

// TestUsageVerbOpensThePaneAndEscCloses pins the verb's routing: /usage opens the pane and launches
// nothing, the frame budgets it as one of its own (openPanes), and esc — its only key — closes it
// while leaving the draft in the box untouched.
func TestUsageVerbOpensThePaneAndEscCloses(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/usage")
	m, cmd := stepCmd(t, m, keyEnter())

	if !m.usagePane.open {
		t.Fatal("/usage did not open the report")
	}
	if m.state != stateIdle || cmd != nil {
		t.Errorf("state = %v, cmd = %v; /usage drives no worker", m.state, cmd)
	}
	if !m.openPanes().has(paneUsage) {
		t.Error("the open report is not in the frame's pane set — it would be drawn on rows nothing budgeted")
	}
	if !strings.Contains(strip(m.frameOverlays().usage), usageTitle) {
		t.Error("the frame does not stack the report it opened")
	}

	m.input.SetValue("half a draft")
	if m = step(t, m, keyEsc()); m.usagePane.open {
		t.Error("esc did not close the report")
	}
	if got := m.input.Value(); got != "half a draft" {
		t.Errorf("draft = %q, want it untouched — the report is a note, not a modal", got)
	}
}

// usageScrollModel opens the report over more delegates than the pane can seat rows for, on a
// transcript long enough to scroll behind it — the state every keyboard assertion below is about.
// The two lists are deliberately both scrollable: what the keys must do is move the one in front.
func usageScrollModel(t *testing.T) Model {
	t.Helper()
	m := usageReportModel(t, 40)
	m.transcript.addUser("a question", nil)
	for range 40 {
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), runRef{})
	}
	m.refreshViewport()
	return m
}

// keyPgUp and keyPgDown are the page keys the report and the transcript both answer to.
func keyPgUp() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyPgUp} }
func keyPgDown() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyPgDown} }

// TestUsageKeysScrollTheReport pins the keyboard the wheel used to be alone in doing: ↑/↓ move the
// window a row, pgup/pgdown a drawn window, and both clamp at the two ends the wheel clamps at — the
// first row, and the last FULL window. The hint says so on the pane itself.
func TestUsageKeysScrollTheReport(t *testing.T) {
	m := usageScrollModel(t)
	win, ok := m.usageWindow()
	if !ok {
		t.Fatal("the report reports no window")
	}
	seats := win.end - win.start
	if win.start != 0 || win.total < 2*seats {
		t.Fatalf("precondition: window [%d,%d) of %d rows — the report must open at the top with more than a page below it",
			win.start, win.end, win.total)
	}
	if pane := strip(m.renderUsage()); !strings.Contains(pane, "↑/↓ scroll · esc close") {
		t.Errorf("the pane does not spell the keys it now owns:\n%s", pane)
	}

	t.Run("the arrows move one row and clamp at the top", func(t *testing.T) {
		down := step(t, m, keyDown())
		if down.usagePane.top != 1 {
			t.Fatalf("top = %d after ↓, want 1", down.usagePane.top)
		}
		if back := step(t, step(t, down, keyUp()), keyUp()); back.usagePane.top != 0 {
			t.Errorf("top = %d after stepping past the first row, want it clamped at 0", back.usagePane.top)
		}
	})

	t.Run("the page keys move a window and clamp at the end", func(t *testing.T) {
		page := step(t, m, keyPgDown())
		if page.usagePane.top != seats {
			t.Errorf("top = %d after pgdn, want a full window of %d rows", page.usagePane.top, seats)
		}
		if up := step(t, m, keyPgUp()); up.usagePane.top != 0 {
			t.Errorf("top = %d after pgup at the top, want it clamped at 0", up.usagePane.top)
		}

		end := m
		for range win.total {
			end = step(t, end, keyPgDown())
		}
		last, ok := end.usageWindow()
		if !ok {
			t.Fatal("the scrolled report reports no window")
		}
		if last.end != last.total || last.end-last.start != seats {
			t.Errorf("paged to the end the window is [%d,%d) of %d rows, want a full %d ending on the last row",
				last.start, last.end, last.total, seats)
		}
	})

	t.Run("esc closes the scrolled report and leaves nothing behind", func(t *testing.T) {
		closed := step(t, step(t, m, keyDown()), keyEsc())
		if closed.usagePane.open {
			t.Error("esc did not close the report")
		}
		if closed.usagePane.top != 0 {
			t.Errorf("top = %d after closing, want the next report opened at the first row", closed.usagePane.top)
		}
	})
}

// TestUsageKeysLeaveTheRestOfTheFrameAlone pins the other half of the claim: the page keys scroll the
// REPORT rather than the conversation hidden behind it — the transcript owns them again the moment it
// is closed — and a printable key still reaches the input box, because the pane is not modal.
func TestUsageKeysLeaveTheRestOfTheFrameAlone(t *testing.T) {
	m := usageScrollModel(t)

	if control := step(t, m.dismissUsage(), keyPgUp()); !control.detached {
		t.Fatalf("precondition: with the report closed pgup did not scroll the transcript (offset %d)",
			control.viewport.YOffset())
	}

	open := step(t, m, keyPgUp())
	if open.detached || open.viewport.YOffset() != m.viewport.YOffset() {
		t.Errorf("pgup moved the transcript behind the report to offset %d (detached=%v), want it untouched at %d",
			open.viewport.YOffset(), open.detached, m.viewport.YOffset())
	}

	typed := step(t, m, keyRune('x'))
	if got := typed.input.Value(); got != "x" {
		t.Errorf("the box holds %q after typing over the report, want %q — the pane is not modal", got, "x")
	}
	if !typed.usagePane.open {
		t.Error("typing closed the report")
	}
}

// TestUsageCachedColumnIsDrawnOnlyWhereAServerReportedOne pins the pane's self-hiding column: a
// cache share is a fact only some servers state, so the column is present when one did and absent
// when none did — a header over a column of blanks would send the reader looking for a number
// nobody said. Present, it is one column for the WHOLE pane: an agent that reported no share
// leaves its cell empty rather than shortening its row out of the columns beside it.
func TestUsageCachedColumnIsDrawnOnlyWhereAServerReportedOne(t *testing.T) {
	t.Run("no agent reported a share", func(t *testing.T) {
		rows := usageModel(t, mainTotals, 8192).usageRows()

		if got := rows[0]; !equalRow(got, usageHeaderCells(false)) {
			t.Errorf("header = %q, want the columns without a cached one %q", got, usageHeaderCells(false))
		}
		if got := len(rows[1]); got != len(usageHeaderCells(false)) {
			t.Errorf("main row has %d cells, want %d — one per column", got, len(usageHeaderCells(false)))
		}
	})

	t.Run("one agent reported a share", func(t *testing.T) {
		cachedMain := mainTotals
		cachedMain.CachedPromptTokens = 12000
		m := usageModel(t, cachedMain, 8192)
		m = delegate(t, m, "s1", "survey the tests", childTotals, 0)
		rows := m.usageRows()

		if got := rows[0]; !equalRow(got, usageHeaderCells(true)) {
			t.Fatalf("header = %q, want the cached column %q", got, usageHeaderCells(true))
		}
		if got, want := rows[1][3], format.Tokens(12000); got != want {
			t.Errorf("main cached cell = %q, want %q", got, want)
		}
		if got := rows[2][3]; got != "" {
			t.Errorf("the delegate's cached cell = %q, want it empty — its server reported no share", got)
		}
		if got, want := rows[3][3], format.Tokens(12000); got != want {
			t.Errorf("session cached cell = %q, want %q — the one share reported, summed", got, want)
		}
	})
}

// TestUsageRestoredDelegateKeepsItsCachedShare is the transcript round trip asserted where a
// reader meets it. The pane draws its cached column for the WHOLE report (usageRows), and a resumed
// run head is never re-fed by a live child — the share reaches its row only through the record
// (session.Entry.UsageCachedPromptTokens). A wire that dropped it would leave that one cell blank under
// a column its neighbours still fill, which reads as a delegate whose server stated no share rather
// than as a number the record lost.
func TestUsageRestoredDelegateKeepsItsCachedShare(t *testing.T) {
	child := childTotals
	child.CachedPromptTokens = 300

	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	tr.applyUsage(childUsage("s1", 1, child.TotalTokens, child), 32768, "")
	subAgentReport(tr, "s1", "tests read", 0)

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	entries, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}

	m := usageModel(t, mainTotals, 8192) // the main agent itself reported no share
	m.transcript.entries = entries
	rows := m.usageRows()

	if len(rows) != 4 {
		t.Fatalf("rows = %q, want the header, the main agent, the restored delegate and the session", rows)
	}
	if got, want := rows[0], usageHeaderCells(true); !equalRow(got, want) {
		t.Fatalf("header = %q, want the cached column %q — the restored share is what opens it", got, want)
	}
	subs := m.usageSubAgentRows(true)
	if len(subs) != 1 {
		t.Fatalf("subAgent rows = %q, want the one restored delegate", subs)
	}
	if got, want := subs[0][3], format.Tokens(300); got != want {
		t.Errorf("the restored delegate's cached cell = %q, want %q — the share came back with the record", got, want)
	}
	if got := rows[1][3]; got != "" {
		t.Errorf("the main agent's cached cell = %q, want it empty — it reported no share of its own", got)
	}
}

// TestDelegateUsageTotalPrefersLiveHeadsOverTheResumedReading pins what the record's delegate sum is
// for. A resumed session carries the sum its record stored, and that is the reading until a run head
// reports one of its own — the heads REPLACE it rather than adding to it, or a resumed session whose
// blocks came back with the scrollback would count every delegate twice.
func TestDelegateUsageTotalPrefersLiveHeadsOverTheResumedReading(t *testing.T) {
	m := usageModel(t, mainTotals, 0)
	m.delegateUsage = childTotals

	if got := m.delegateUsageTotal(); got != childTotals {
		t.Errorf("with no run heads the total = %+v, want the resumed record's own %+v", got, childTotals)
	}

	m = delegate(t, m, "s1", "survey the tests", childTotals, 0)

	if got := m.delegateUsageTotal(); got != childTotals {
		t.Errorf("with one live head the total = %+v, want that head's own %+v — the restored sum is replaced",
			got, childTotals)
	}
}

// ----------------------------------------------------------------------------
// The resumed base the engine's reading is folded onto (Model.usageBase)
// ----------------------------------------------------------------------------

// resumedUsageModel builds a model opened on a record that already carried the given totals — the
// startup-resume path, which is where the base is seeded (replayResumed).
func resumedUsageModel(t *testing.T, stored session.Usage) Model {
	t.Helper()
	return newModel(context.Background(), &fakeEngine{}, Options{
		Resumed: &ResumedSession{Title: "france question", Usage: stored},
	}, nil)
}

// TestUsageAccumulatesOverAResumedReading pins the offset half of the session's accounting. The
// engine's cumulative reading is its own running sum SINCE THE SESSION WAS OPENED, and a resume
// restarts it at zero — a fresh Agent on --resume, RestoreSession's reset on a browser restore — so
// the view folds that reading ON TOP of what the record carried in. A fixed base added to each
// latest-wins reading is added once, not once per event, and a session that resumed nothing has no
// base to add.
func TestUsageAccumulatesOverAResumedReading(t *testing.T) {
	t.Parallel()
	stored := session.Usage{Calls: 40, PromptTokens: 480000, CompletionTokens: 20000, TotalTokens: 500000}

	t.Run("the first post-resume reading adds to the record's totals", func(t *testing.T) {
		t.Parallel()
		m := resumedUsageModel(t, stored)

		m = m.foldEvent(mainUsage(5000, 300, 5300, 5000, 300, 5300, 1))

		want := usageTotals{Calls: 41, PromptTokens: 485000, CompletionTokens: 20300, TotalTokens: 505300}
		if m.usage != want {
			t.Errorf("totals = %+v, want %+v — the engine's reading rides on the resumed base", m.usage, want)
		}
		payload, ok := m.snapshotPayload(domain.Session{})
		if !ok {
			t.Fatal("snapshotPayload declined to build a payload")
		}
		if got := payload.usage; got != session.Usage(want) {
			t.Errorf("saved totals = %+v, want the same %+v the pane reports", got, want)
		}
	})

	t.Run("the base is added once, not once per event", func(t *testing.T) {
		t.Parallel()
		m := resumedUsageModel(t, stored)

		m = m.foldEvent(mainUsage(5000, 300, 5300, 5000, 300, 5300, 1))
		m = m.foldEvent(mainUsage(4000, 400, 4400, 9000, 700, 9700, 2))

		want := usageTotals{Calls: 42, PromptTokens: 489000, CompletionTokens: 20700, TotalTokens: 509700}
		if m.usage != want {
			t.Errorf("totals = %+v, want %+v — a latest-wins reading plus one fixed offset", m.usage, want)
		}
	})

	t.Run("a session that resumed nothing reports the reading alone", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t)

		m = m.foldEvent(mainUsage(5000, 300, 5300, 5000, 300, 5300, 1))

		want := usageTotals{Calls: 1, PromptTokens: 5000, CompletionTokens: 300, TotalTokens: 5300}
		if m.usage != want {
			t.Errorf("totals = %+v, want exactly the event's reading %+v — a fresh launch has no base", m.usage, want)
		}
	})
}
