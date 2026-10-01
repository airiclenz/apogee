package tui

import (
	"context"
	"fmt"
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
func usageModel(t *testing.T, totals domain.Usage, used int) Model {
	t.Helper()
	m := newTestModel(t)
	m.usage = totals
	m.ctxUsed = used
	m.usagePane = usagePane{open: true}
	return m
}

// delegate adds a sub-agent run to the transcript with the totals and fill its child reported.
func delegate(t *testing.T, m Model, id, task string, totals domain.Usage, used int) Model {
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
	mainTotals  = domain.Usage{Calls: 3, PromptTokens: 20000, CompletionTokens: 1500, TotalTokens: 21500}
	childTotals = domain.Usage{Calls: 2, PromptTokens: 4000, CompletionTokens: 500, TotalTokens: 4500}
)

// TestReportPaneBreathes pins the house blanks on a REPORT pane, over the family's own painter
// (Model.reportSpec, which /usage, /inspect and /thinking all compose through): an overflowing
// reading stands one blank line clear of the title above it and one clear of the key legend below
// it, both bought out of the ROW WINDOW rather than out of the pane's height — the pane is exactly
// as tall as it was and shows two rows fewer — and neither blank carries a cell of the scrollbar
// that runs beside the rows. At the pane's floor the two are handed back together and the reading
// is what stays.
func TestReportPaneBreathes(t *testing.T) {
	t.Parallel()

	m := usageModel(t, mainTotals, 8192)
	for i := range maxUsageRows {
		m = delegate(t, m, fmt.Sprintf("s%d", i), fmt.Sprintf("delegate %d", i), childTotals, 4096)
	}

	t.Run("a reading longer than its window is set off at both ends", func(t *testing.T) {
		wide := step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

		spec, seated := wide.reportSpec(usageReport, usageContent(wide.usageRows(), wide.servedModels))
		if !seated || len(spec.rows) <= spec.maxRows {
			t.Fatalf("precondition: %d rows into a %d-line window (seated=%v) — the reading must overflow the pane",
				len(spec.rows), spec.maxRows, seated)
		}
		view := wide.renderReport(usageReport)
		lines := popupLines(view)

		if got := popupInterior(lines[2]); got != "" {
			t.Errorf("the line under the title is %q, want the blank the block opens on", got)
		}
		if got := popupInterior(lines[len(lines)-3]); got != "" {
			t.Errorf("the line over the key legend is %q, want the blank the block closes on", got)
		}
		if got := popupInterior(lines[len(lines)-2]); got != usageHint {
			t.Errorf("the pane closes on %q, want its key legend %q", got, usageHint)
		}
		// The two borders, the title, the hint and the two blanks are everything that is not a row.
		rows := len(lines) - 6
		if want := spec.maxRows - 2; rows != want {
			t.Errorf("the pane seats %d rows out of the %d lines the frame granted its block, want %d — the blanks come out of the row window, not out of the frame",
				rows, spec.maxRows, want)
		}
		if got := len(popupBarColumn(view)); got != rows {
			t.Errorf("the scrollbar runs %d cells beside %d rows — it reaches onto a breathing blank", got, rows)
		}
	})

	t.Run("a window down to its floor keeps the reading and drops the blanks", func(t *testing.T) {
		// smallestOverlayWindow is the shortest window a boxed pane fits in at all; 17 and 18 are
		// where this pane's block is down to one row and to two, which is where the painter hands
		// the pads back rather than spend a row of the reading on them (popupRowLinesAt).
		for _, height := range []int{smallestOverlayWindow, 17, 18} {
			short := step(t, m, tea.WindowSizeMsg{Width: 80, Height: height})

			spec, seated := short.reportSpec(usageReport, usageContent(short.usageRows(), short.servedModels))
			if !seated {
				t.Fatalf("the frame seated no pane at %d rows", height)
			}
			lines := popupLines(short.renderReport(usageReport))

			for i, line := range lines[1 : len(lines)-1] {
				if popupInterior(line) == "" {
					t.Errorf("at %d rows the pane paints a blank content line at %d, want its breathing room handed back to the reading:\n%s",
						height, i+1, strip(short.renderReport(usageReport)))
				}
			}
			// The two borders, the title and the hint: what is left is the reading itself.
			if rows := len(lines) - 4; rows != spec.maxRows {
				t.Errorf("at %d rows the pane seats %d rows of the %d the frame granted", height, rows, spec.maxRows)
			}
		}
	})
}

// TestUsageRowsReportEveryAgentThatSpent pins what the report is made of: the column header, the
// main agent, each delegate that reported a count — in transcript order — and the session total,
// which appears only where there is more than one agent to add up.
func TestUsageRowsReportEveryAgentThatSpent(t *testing.T) {
	t.Parallel()

	t.Run("the main agent alone gets no session row", func(t *testing.T) {
		t.Parallel()

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
		t.Parallel()

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

		sum := domain.Sum(mainTotals, childTotals, childTotals)
		want := popupRow{usageSessionLabel, "7",
			format.Tokens(sum.PromptTokens), format.Tokens(sum.CompletionTokens), format.Tokens(sum.TotalTokens), ""}
		if !equalRow(rows[4], want) {
			t.Errorf("session row = %q, want %q — the agents' own totals added up", rows[4], want)
		}
	})

	t.Run("a delegate that reported nothing is left out", func(t *testing.T) {
		t.Parallel()

		m := usageModel(t, mainTotals, 8192)
		m = delegate(t, m, "s1", "survey the tests", domain.Usage{}, 0)

		if rows := m.usageRows(); len(rows) != 2 {
			t.Errorf("rows = %q, want the header and the main agent — a run with no count is not a spend", rows)
		}
	})

	t.Run("nothing reported at all is no rows", func(t *testing.T) {
		t.Parallel()

		if rows := usageModel(t, domain.Usage{}, 0).usageRows(); rows != nil {
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
	t.Parallel()

	m := usageModel(t, mainTotals, 8192)
	m = delegate(t, m, "s1", "survey the tests", childTotals, 16384)
	pane := strip(m.renderReport(usageReport))

	for _, want := range []string{usageTitle, usageHint, usageMainLabel, usageSessionLabel,
		"survey the tests", usageHeaderCells(false)[1], format.Tokens(21500)} {
		if !strings.Contains(pane, want) {
			t.Errorf("the pane does not show %q:\n%s", want, pane)
		}
	}

	empty := strip(usageModel(t, domain.Usage{}, 0).renderReport(usageReport))
	if !strings.Contains(empty, usageEmptyBody) {
		t.Errorf("the empty pane does not say why it is empty:\n%s", empty)
	}
	if strings.Contains(empty, usageMainLabel) {
		t.Errorf("the empty pane drew an agent row that reported nothing:\n%s", empty)
	}

	if closed := m.closedUsage().renderReport(usageReport); closed != "" {
		t.Errorf("the closed pane rendered %q, want nothing", closed)
	}
}

// TestUsagePaneNamesTheModelsThatAnswered pins the served line: the pane says `served: a, b` above
// its rows once a reply has named a model, in the order the session first saw them, and carries no
// such line while no reply has — a server that names no model leaves the pane exactly as it was.
func TestUsagePaneNamesTheModelsThatAnswered(t *testing.T) {
	t.Parallel()

	m := usageModel(t, mainTotals, 8192)
	m.servedModels = []string{"gpt-oss-20b-mxfp4", "grunt-8b"}

	pane := strip(m.renderReport(usageReport))
	if want := usageServedLabel + "gpt-oss-20b-mxfp4" + usageServedSeparator + "grunt-8b"; !strings.Contains(pane, want) {
		t.Errorf("the pane does not name the models that answered %q:\n%s", want, pane)
	}
	if !strings.Contains(pane, usageMainLabel) {
		t.Errorf("the served line displaced the rows:\n%s", pane)
	}

	unnamed := strip(usageModel(t, mainTotals, 8192).renderReport(usageReport))
	if strings.Contains(unnamed, usageServedLabel) {
		t.Errorf("the pane drew a served line with no model named:\n%s", unnamed)
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
	t.Parallel()

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
	if !strings.Contains(strip(m.frameOverlays().block(paneUsage)), usageTitle) {
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
	t.Parallel()

	m := usageScrollModel(t)
	win, ok := m.reportWindow(usageReport)
	if !ok {
		t.Fatal("the report reports no window")
	}
	seats := win.end - win.start
	if win.start != 0 || win.total < 2*seats {
		t.Fatalf("precondition: window [%d,%d) of %d rows — the report must open at the top with more than a page below it",
			win.start, win.end, win.total)
	}
	if pane := strip(m.renderReport(usageReport)); !strings.Contains(pane, "↑/↓ scroll · esc close") {
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
		last, ok := end.reportWindow(usageReport)
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
	t.Parallel()

	m := usageScrollModel(t)

	if control := step(t, m.dismissReport(usageReport), keyPgUp()); !control.detached {
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
	t.Parallel()

	t.Run("no agent reported a share", func(t *testing.T) {
		t.Parallel()

		rows := usageModel(t, mainTotals, 8192).usageRows()

		if got := rows[0]; !equalRow(got, usageHeaderCells(false)) {
			t.Errorf("header = %q, want the columns without a cached one %q", got, usageHeaderCells(false))
		}
		if got := len(rows[1]); got != len(usageHeaderCells(false)) {
			t.Errorf("main row has %d cells, want %d — one per column", got, len(usageHeaderCells(false)))
		}
	})

	t.Run("one agent reported a share", func(t *testing.T) {
		t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
		UI:      testUIPrefs,
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

		want := domain.Usage{Calls: 41, PromptTokens: 485000, CompletionTokens: 20300, TotalTokens: 505300}
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

		want := domain.Usage{Calls: 42, PromptTokens: 489000, CompletionTokens: 20700, TotalTokens: 509700}
		if m.usage != want {
			t.Errorf("totals = %+v, want %+v — a latest-wins reading plus one fixed offset", m.usage, want)
		}
	})

	t.Run("a session that resumed nothing reports the reading alone", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t)

		m = m.foldEvent(mainUsage(5000, 300, 5300, 5000, 300, 5300, 1))

		want := domain.Usage{Calls: 1, PromptTokens: 5000, CompletionTokens: 300, TotalTokens: 5300}
		if m.usage != want {
			t.Errorf("totals = %+v, want exactly the event's reading %+v — a fresh launch has no base", m.usage, want)
		}
	})
}

// ----------------------------------------------------------------------------
// The session boundary /clear and /new draw through the accounting (resetSessionView)
// ----------------------------------------------------------------------------

// TestClearResetsTheUsageTallies pins the view half of the /clear boundary. The sums belong to the
// session the verb closes — its record took them before the reset — so the pane and the record of
// the fresh session report only what the fresh session spends: a resumed base is forgotten along
// with the readings folded onto it, the engine's own tally restarts at zero at the same boundary
// (ClearContext), and the first reading after the clear is the whole of the accounting. A delegate
// sum a resumed record carried falls with it under /new, the alias that shares the seam.
func TestClearResetsTheUsageTallies(t *testing.T) {
	t.Parallel()
	stored := session.Usage{Calls: 40, PromptTokens: 480000, CompletionTokens: 20000, TotalTokens: 500000}

	t.Run("/clear then one reply reports that reply alone", func(t *testing.T) {
		t.Parallel()
		m := resumedUsageModel(t, stored)
		m = m.foldEvent(mainUsage(5000, 300, 5300, 5000, 300, 5300, 1))
		m = m.foldEvent(servedUsage("resumed-answerer", 0))
		if m.usage.Calls != 41 {
			t.Fatalf("precondition: the resumed session has spent %+v, want 41 calls to clear away", m.usage)
		}

		m.input.SetValue("/clear")
		m = step(t, m, keyEnter())

		if m.usage != (domain.Usage{}) || m.usageBase != (domain.Usage{}) {
			t.Fatalf("after /clear usage = %+v, base = %+v, want both zero — the spend went with the closed session",
				m.usage, m.usageBase)
		}
		if m.servedModels != nil {
			t.Errorf("after /clear servedModels = %q, want none — the closed session's answerers went with its tallies", m.servedModels)
		}
		m = m.foldEvent(mainUsage(700, 40, 740, 700, 40, 740, 1))

		want := domain.Usage{Calls: 1, PromptTokens: 700, CompletionTokens: 40, TotalTokens: 740}
		if m.usage != want {
			t.Errorf("totals = %+v, want exactly the reply's own %+v — nothing of the closed session is added", m.usage, want)
		}
		m.usagePane = usagePane{open: true}
		rows := m.usageRows()
		if len(rows) != 2 {
			t.Fatalf("rows = %q, want the header and the main agent alone", rows)
		}
		if got, want := rows[1], usageRow(usageMainLabel, want, m.ctxUsed, m.opts.ContextWindow, false); !equalRow(got, want) {
			t.Errorf("the main row = %q, want %q — the pane reads the reply alone", got, want)
		}
		payload, ok := m.snapshotPayload(domain.Session{})
		if !ok {
			t.Fatal("snapshotPayload declined to build a payload")
		}
		if got := payload.usage; got != session.Usage(want) {
			t.Errorf("saved totals = %+v, want the same %+v the pane reports", got, want)
		}
	})

	t.Run("/new drops the delegate sum a resumed record carried", func(t *testing.T) {
		t.Parallel()
		m := resumedUsageModel(t, stored)
		m.delegateUsage = childTotals
		if got := m.delegateUsageTotal(); got != childTotals {
			t.Fatalf("precondition: the delegate total = %+v, want the record's %+v", got, childTotals)
		}

		m.input.SetValue("/new")
		m = step(t, m, keyEnter())

		if m.delegateUsage != (domain.Usage{}) {
			t.Errorf("delegateUsage after /new = %+v, want zero — the closed session's delegates spent it", m.delegateUsage)
		}
		if got := m.delegateUsageTotal(); got != (domain.Usage{}) {
			t.Errorf("delegateUsageTotal after /new = %+v, want zero — no run of the fresh session has reported", got)
		}
		payload, ok := m.snapshotPayload(domain.Session{})
		if !ok {
			t.Fatal("snapshotPayload declined to build a payload")
		}
		if payload.usage != (session.Usage{}) || payload.delegateUsage != (session.Usage{}) {
			t.Errorf("saved usage = %+v, delegate = %+v, want both zero — the fresh record starts from nothing",
				payload.usage, payload.delegateUsage)
		}
	})
}

// ----------------------------------------------------------------------------
// A Workflow's spend — its item runs and the runs they spawned (delegateUsageHeads)
// ----------------------------------------------------------------------------

// workflowRunUsage is one cumulative reading a Workflow's run reports: calls completions spending
// prompt and completion tokens so far, the reading that restates its running sum.
func workflowRunUsage(base domain.EventBase, calls, prompt, completion int) domain.UsageEvent {
	total := prompt + completion
	return domain.UsageEvent{
		EventBase:   base,
		TotalTokens: total,
		Cumulative:  domain.Usage{Calls: calls, PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total},
	}
}

// workflowRow is the /usage row of a delegate named name that spent totals and reported no fill —
// the row a Workflow gets.
func workflowRow(name string, totals domain.Usage) popupRow {
	return usageRow(usageIndent+name, totals, 0, 0, false)
}

// fanOutCall is the blocking fan_out call f1, whose Workflow's item runs are bracketed under it.
func fanOutCall() domain.ToolCallEvent {
	return domain.ToolCallEvent{Call: domain.ToolCall{ID: "f1", Tool: fanOutToolName, Arguments: []byte(`{"task":"check {item}"}`)}}
}

// assertDelegateRows fails unless the /usage rows below the main agent's are want, in order, then
// the session row adding delegates to the main agent's totals — and unless the record a save takes
// now stores delegates as its delegate half, the sum the pane adds.
func assertDelegateRows(t *testing.T, m Model, delegates domain.Usage, want ...popupRow) {
	t.Helper()
	rows := m.usageRows()
	if len(rows) != len(want)+3 {
		t.Fatalf("rows = %q, want header, main, %d delegate rows and the session total", rows, len(want))
	}
	for i, row := range want {
		if got := rows[2+i]; !equalRow(got, row) {
			t.Errorf("delegate row %d = %q, want %q", i, got, row)
		}
	}
	session := usageRow(usageSessionLabel, domain.Sum(m.usage, delegates), 0, 0, false)
	if got := rows[len(rows)-1]; !equalRow(got, session) {
		t.Errorf("session row = %q, want %q — the delegates added to the main agent once", got, session)
	}
	payload, ok := m.snapshotPayload(domain.Session{})
	if !ok {
		t.Fatal("snapshotPayload declined to build a payload")
	}
	if got := domain.Usage(payload.delegateUsage); got != delegates {
		t.Errorf("saved delegate usage = %+v, want the pane's own %+v", got, delegates)
	}
}

// A background workflow's spend — each item run's latest reading, and a run one of them spawned —
// is one /usage row named for the workflow, added into the session row and the record's delegate
// half, while the main agent's totals stay its own. Once it finishes, its finish line carries the
// sum, and a save and reopen keeps the row, its name and the session total.
func TestBackgroundWorkflowSpendReachesUsageAndTheRecord(t *testing.T) {
	t.Parallel()
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	m.usage = mainTotals
	nested := domain.EventBase{Depth: 2, Turn: 1, CallID: "c1", RunID: "bg.3"}

	m = foldEvents(t, m,
		bgPhase(domain.WorkflowStarted),
		workflowRunUsage(bgChildBase("bg.1"), 1, 1000, 100),
		workflowRunUsage(bgChildBase("bg.2"), 1, 800, 80),
		domain.ToolCallEvent{EventBase: bgChildBase("bg.2"), Call: domain.ToolCall{ID: "c1", Tool: "sub_agent"}, SpawnRunID: "bg.3"},
		workflowRunUsage(nested, 1, 400, 40),
		workflowRunUsage(bgChildBase("bg.1"), 2, 2500, 300), // restates bg.1's running sum
	)

	spent := domain.Usage{Calls: 4, PromptTokens: 3700, CompletionTokens: 420, TotalTokens: 4120}
	if m.usage != mainTotals {
		t.Errorf("main totals = %+v, want %+v untouched — a background run's reading is not the main agent's", m.usage, mainTotals)
	}
	assertDelegateRows(t, m, spent, workflowRow(bgWorkflowName, spent))

	m = foldEvents(t, m, bgItemOK(), bgPhase(domain.WorkflowFinished))
	assertDelegateRows(t, m, spent, workflowRow(bgWorkflowName, spent))

	payload, ok := m.snapshotPayload(domain.Session{})
	if !ok {
		t.Fatal("snapshotPayload declined to build a payload")
	}
	reopened := newModel(context.Background(), &fakeEngine{}, Options{
		Resumed: &ResumedSession{
			Transcript: payload.transcript, Usage: payload.usage, DelegateUsage: payload.delegateUsage,
		},
		UI: testUIPrefs,
	}, nil)
	assertDelegateRows(t, reopened, spent, workflowRow(bgWorkflowName, spent))
}

// A foreground Workflow's item runs fold into the block that started it — a blocking fan_out's card
// or a Recipe launch's workflow block — each run's latest reading kept and the runs summed into one
// row named for the Workflow.
func TestForegroundWorkflowSpendReachesUsage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		open  []domain.Event
		call  string
		label string
		runs  []string
		want  domain.Usage
	}{
		{
			name: "a blocking fan_out's two item runs", open: []domain.Event{fanOutCall(), startedUnder("f1")},
			call: "f1", label: "check {item}", runs: []string{"run.1", "run.2"},
			want: domain.Usage{Calls: 4, PromptTokens: 5000, CompletionTokens: 600, TotalTokens: 5600},
		},
		{
			name: "a recipe launch's item run", open: []domain.Event{startedUnder("w1")},
			call: "w1", label: "audit", runs: []string{"run.1"},
			want: domain.Usage{Calls: 2, PromptTokens: 2500, CompletionTokens: 300, TotalTokens: 2800},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := usageModel(t, mainTotals, 0)
			for _, e := range tc.open {
				m = m.foldEvent(e)
			}

			for _, run := range tc.runs {
				m = m.foldEvent(workflowRunUsage(itemBase(tc.call, run), 1, 1000, 100))
				m = m.foldEvent(workflowRunUsage(itemBase(tc.call, run), 2, 2500, 300)) // restates the first
			}

			if m.usage != mainTotals {
				t.Errorf("main totals = %+v, want %+v untouched", m.usage, mainTotals)
			}
			assertDelegateRows(t, m, tc.want, workflowRow(tc.label, tc.want))
		})
	}
}

// A Workflow whose item runs each have a run head of their own (WorkflowItemStarted) still reports
// ONE /usage row, named for the Workflow and summing its runs: the item heads wear their runs' fill
// and are never listed as sub-agents of their own.
func TestSeatedWorkflowItemsReportOneWorkflowRow(t *testing.T) {
	t.Parallel()
	m := usageModel(t, mainTotals, 0)
	for _, e := range []domain.Event{
		fanOutCall(),
		startedUnder("f1"),
		itemStartedUnder("f1", "run.1", "items", "alpha", 0, 1),
		itemStartedUnder("f1", "run.2", "items", "beta", 1, 1),
		workflowRunUsage(itemBase("f1", "run.1"), 1, 1000, 100),
		workflowRunUsage(itemBase("f1", "run.2"), 1, 2000, 200),
	} {
		m = m.foldEvent(e)
	}

	want := domain.Usage{Calls: 2, PromptTokens: 3000, CompletionTokens: 300, TotalTokens: 3300}
	assertDelegateRows(t, m, want, workflowRow("check {item}", want))
	if head := m.transcript.entries[workflowItemHeadAt(m.transcript.entries, "run.2")]; head.ctxUsed != 2200 {
		t.Errorf("beta's item head fill = %d, want its run's own 2200", head.ctxUsed)
	}
}

// A run an item spawned through sub_agent keeps its own head: its reading is that head's row and
// never also the Workflow's, so the session counts it once.
func TestWorkflowItemsSubAgentCountsOnceUnderItsOwnHead(t *testing.T) {
	t.Parallel()
	m := usageModel(t, mainTotals, 0)
	child := domain.EventBase{Depth: 2, CallID: "s1", RunID: "run.3"}

	for _, e := range []domain.Event{
		fanOutCall(),
		workflowRunUsage(itemBase("f1", "run.1"), 1, 1000, 100),
		domain.ToolCallEvent{
			EventBase:  itemBase("f1", "run.1"),
			Call:       domain.ToolCall{ID: "s1", Tool: "sub_agent", Arguments: []byte(`{"task":"dig deeper"}`)},
			SpawnRunID: "run.3",
		},
		workflowRunUsage(child, 1, 400, 40),
	} {
		m = m.foldEvent(e)
	}

	item := domain.Usage{Calls: 1, PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100}
	sub := domain.Usage{Calls: 1, PromptTokens: 400, CompletionTokens: 40, TotalTokens: 440}
	assertDelegateRows(t, m, domain.Sum(item, sub),
		usageRow(usageIndent+"dig deeper", sub, 440, m.opts.ContextWindow, false),
		workflowRow("check {item}", item),
	)
}

// /usage opens the report and returns: the pane is laid out by Update's tail (doc.go, "an arm
// mutates").
func TestUsagePaneIsSettledByTheTail(t *testing.T) {
	t.Parallel()
	m, _ := typeCommand(t, newTestModel(t), "/usage")
	if !m.usagePane.open {
		t.Fatal("precondition: /usage did not open the report")
	}
	assertSettled(t, m)
}
