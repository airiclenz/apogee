package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/format"
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
		if got, want := rows[0], usageHeaderCells; !equalRow(got, want) {
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
		"survey the tests", usageHeaderCells[1], format.Tokens(21500)} {
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
