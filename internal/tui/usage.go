package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/format"
)

// ----------------------------------------------------------------------------
// /usage — the session's token accounting, per agent (command.go owns the verb)
// ----------------------------------------------------------------------------
//
// A read-only report of what this session has SPENT: one row for the main agent, one for every
// sub-agent that reported usage, and — where there is more than one agent to add up — a session
// total. It answers the question the status gauge cannot, because the gauge is a FILL: how full the
// window is right now says nothing about the tokens a run burned through and compacted away, and it
// says nothing at all about a delegate whose window closed with its run.
//
// The numbers are not derived here and nothing is summed over events: each agent keeps its own
// running total in the engine and stamps it on every UsageEvent it emits (domain.UsageEvent), the
// folds keep the LATEST reading per agent (foldStats for the main agent, transcript.applyUsage for
// each run head), and this pane reads those readings out. That is what makes the report survive a
// dropped event, a resumed session (the main agent's totals ride the session record, and the fold
// adds the engine's post-resume reading onto that stored base — Model.usageBase — because the
// engine's own count restarts at zero on every resume) and a compaction (whose tokens arrive as a
// flagged maintenance event the gauge skips and the totals take).
//
// It is the lightest pane in the frame: no filter, no selection, and the only keys it owns are esc
// and the four that scroll it. The verb is whileRunning — the pane reads Model state and calls
// nothing — so it opens over a working agent, which is exactly when the question gets asked.
//
// The PANE is not written here. A report — the key contract, the dismiss, the budget→render path and
// the whole mouse family — is one shape the frame holds twice (reportpane.go, shared with the
// /inspect view), so what is left in this file is the only thing that is this report's alone: what it
// says. The entries below are that module's functions under this pane's own name.

// usagePane is the /usage report overlay's state — a reportPane (reportpane.go) under the name of the
// pane that keeps it: whether the report is up, and how far its row list is scrolled. It is the one
// report that does NOT follow the tail of its rows ([reportKind.follows]): a delegate row arriving
// while it is open leaves the window where the reader put it.
type usagePane = reportPane

// usageTitle names the pane, and usageHint spells the keys it owns.
const (
	usageTitle = "session token usage"
	usageHint  = "↑/↓ scroll · esc close"
)

// usageEmptyBody is what the pane says when no agent has reported a token count yet — a fresh
// session, or a server that omits usage entirely. It is BODY prose rather than a row, because a row
// would be read as an agent under the column headers the pane would then have to draw over nothing.
const usageEmptyBody = "no usage reported yet — no completion has come back with a token count"

// maxUsageRows is the pane's own taste for how many rows it shows at once: the header, a main row
// and a session total leave nine for delegates, which is more than a session fans out to in one
// conversation. [Model.popupBudget] cuts it down to what the window can seat.
const maxUsageRows = 12

// usageNameCells is how wide a delegate's name may be before it is clipped. A name is the one cell
// here whose width the pane does not control — it is the `name` a model gave the delegation, or the
// first line of the task where it named none — and the numeric columns beside it need the room
// more than a long task line does.
const usageNameCells = 24

// usageIndent leads a delegate's row so the main agent's reads as the one the rest hang under, the
// nesting the transcript already paints their blocks with.
const usageIndent = "  "

// usageMainLabel, usageSessionLabel and usageAgentFallback are the pane's own names for the rows
// nothing else names: the conversation the human is steering, the sum of every row above, and a
// delegation that carried neither a name nor a task to take one from.
const (
	usageMainLabel     = "main"
	usageSessionLabel  = "session"
	usageAgentFallback = "sub-agent"
)

// usageHeaderCells are the column labels, in the order ratified for this pane (plan
// "2026-08-11 - 02", design call 4). They are a row of the spec rather than body prose so the
// popup module's own column machinery aligns them over the cells they name.
//
// The cached column is drawn only when some agent reported a cache share, and every row on the
// pane answers to that one verdict so the columns stay square (usageRows). A pane that always
// carried it would head a column of blanks on the servers that report no breakdown at all — which
// is nearly all of them — and a reader would scan that column for a number no server ever said.
func usageHeaderCells(cached bool) popupRow {
	if cached {
		return popupRow{"agent", "calls", "prompt", "cached", "completion", "total", "ctx"}
	}
	return popupRow{"agent", "calls", "prompt", "completion", "total", "ctx"}
}

// runUsageCommand drives the /usage verb: it opens the pane and does nothing else. Synchronous like
// /settings and /sessions — no engine call, no worker, no I/O — and, unlike either of them, safe
// while a worker works: everything it shows is already on the Model.
//
// It opens on an empty session too, where /settings would decline: an empty settings pane is a
// modal the human has to escape, while this one is a one-line answer to a question they asked, and
// "nothing has been counted yet" IS that answer.
func (m Model) runUsageCommand() (tea.Model, tea.Cmd) {
	m.usagePane = usagePane{open: true}
	m.layout()
	return m, nil
}

// The report module's functions under this pane's name (reportpane.go). Each one is the shared body
// with usageReport filled in: naming them here is what lets the frame, the keyboard and the pointer
// go on addressing the /usage report as themselves while there is only one report left to maintain.

// renderUsage paints the pane, or "" when it is closed or the frame cannot seat it.
func (m Model) renderUsage() string { return m.renderReport(usageReport) }

// usageSpec composes the report's [popupSpec] for THIS frame out of rows its caller has already
// composed — the pane's own entry into [Model.reportSpec], which the window and the paint reach
// through [Model.reportContent] instead.
func (m Model) usageSpec(rows []popupRow) (popupSpec, bool) {
	return m.reportSpec(usageReport, usageContent(rows))
}

// usageKey is the pane's whole key contract: esc closes the report, ↑/↓ scroll it a row at a time and
// pgup/pgdown a drawn window at a time (reportKey).
func (m Model) usageKey(msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	return m.reportKey(usageReport, msg)
}

// dismissUsage takes the report off the frame and gives its rows back to the transcript.
func (m Model) dismissUsage() Model { return m.dismissReport(usageReport) }

// usagePaneRect is where the open report is drawn: the screen row its top border lands on and how
// many rows it takes.
func (m Model) usagePaneRect() (y0, h int, ok bool) { return m.reportPaneRect(usageReport) }

// usageWindow is the row window the report is showing as the frame DREW it.
func (m Model) usageWindow() (reportWindow, bool) { return m.reportWindow(usageReport) }

// handleUsageClick answers a left-click while the report is up: inside the box it is claimed and
// nothing happens, outside it the report is dismissed and the click goes on.
func (m Model) handleUsageClick(pre Model, msg tea.MouseClickMsg) (Model, bool) {
	return m.handleReportClick(usageReport, pre, msg)
}

// usageWheel scrolls the report one row per notch while the pointer is over it.
func (m Model) usageWheel(msg tea.MouseWheelMsg) (Model, bool) {
	return m.reportWheel(usageReport, msg)
}

// usageContent is what the report tells the shared module about itself for one frame: its name, the
// keys it spells, how tall it likes to be, the rows it was composed with, and — where there are none
// — the one sentence it shows instead of them.
func usageContent(rows []popupRow) reportContent {
	body := ""
	if len(rows) == 0 {
		body = usageEmptyBody
	}
	return reportContent{
		title:  usageTitle,
		hint:   usageHint,
		rowCap: maxUsageRows,
		body:   body,
		rows:   rows,
		kinds:  usageRowKinds(len(rows)),
	}
}

// usageRowKinds marks the first row as the column header and leaves every other row plain — the
// heading kind the /settings pane's section labels already use, so the labels are found without
// being read. It is short by one row for the empty pane, where there are no rows to mark at all.
func usageRowKinds(rows int) []popupRowKind {
	if rows == 0 {
		return nil
	}
	kinds := make([]popupRowKind, rows)
	kinds[0] = popupRowHeading
	return kinds
}

// usageRows composes the report: the column header, the main agent, every delegate that reported a
// count, and — only where a delegate row stands — the session total. NOTHING at all when no agent
// has reported, which is the empty state usageContent words instead.
//
// A delegate that reported no count is left out rather than shown as a row of blanks: a run whose
// child never got a usage report back is a fact about the server, not a spend, and a row of empty
// cells under the totals would read as one.
func (m Model) usageRows() []popupRow {
	delegates := m.delegateUsageTotal()
	// One verdict for the whole pane: a cached cell on one row and none on the next would put two
	// different column counts under one header (usageHeaderCells).
	cached := m.usage.CachedPromptTokens > 0 || delegates.CachedPromptTokens > 0
	subs := m.usageSubAgentRows(cached)
	if m.usage.Calls <= 0 && delegates.Calls <= 0 {
		return nil
	}
	rows := make([]popupRow, 0, len(subs)+3)
	rows = append(rows, usageHeaderCells(cached))
	rows = append(rows, usageRow(usageMainLabel, m.usage, m.ctxUsed, m.opts.ContextWindow, cached))
	rows = append(rows, subs...)
	if delegates.Calls <= 0 {
		return rows
	}
	// The session row is the one number the rows above cannot be read off: an agent's own total is
	// what it spent, and what the SESSION spent is the sum of them. It is drawn only where there is
	// a delegate spend to sum — with none the main row already is the session — and it carries no
	// fill, because two windows do not add up to a third. A resumed record whose delegate spend
	// outlived its blocks is summed here with no row of its own to point at (delegateUsageTotal),
	// which is the honest reading: the tokens were spent by this session, and the runs that spent
	// them are no longer on the pane to be asked.
	return append(rows, usageRow(usageSessionLabel, usageSum(m.usage, delegates), 0, 0, cached))
}

// usageSubAgentRows composes one row per delegate that reported a count, in transcript order — the
// order their blocks stand in, so a reader matches a row to the run above it by position.
func (m Model) usageSubAgentRows(cached bool) []popupRow {
	heads := m.delegateUsageHeads()
	rows := make([]popupRow, 0, len(heads))
	for _, head := range heads {
		name, _ := clipCells(m.th, usageIndent+usageAgentName(head), usageNameCells)
		rows = append(rows, usageRow(name, head.usage, head.ctxUsed, head.ctxLimit, cached))
	}
	return rows
}

// delegateUsageHeads are the sub-agent run heads that reported a count, in transcript order. It is
// the one walk both readers of a delegate's spend take — the pane's rows and the session record's
// delegate sum — so a run that appears on the /usage pane is exactly a run the record counts.
func (m Model) delegateUsageHeads() []entry {
	var heads []entry
	for i := range m.transcript.entries {
		head := m.transcript.entries[i]
		if !head.headsRun() || head.usage.Calls <= 0 {
			continue
		}
		heads = append(heads, head)
	}
	return heads
}

// delegateUsageTotal is what this session's DELEGATES have spent: the sum of the latest reading of
// every run head that reported one. It is what the session record stores beside the main agent's
// accounting (savePayload) and what the pane's session row adds to it, so the two never disagree
// about the same session.
//
// Where no live head reports a count it falls back to the delegate sum a RESUMED record carried
// (Model.delegateUsage), which is the only reading left when a record's scrollback could not be
// repainted — a legacy or undecodable blob replays no run blocks at all. A live head replaces it
// the moment one reports, so the fallback never adds to the runs it is standing in for.
func (m Model) delegateUsageTotal() usageTotals {
	var total usageTotals
	for _, head := range m.delegateUsageHeads() {
		total = usageSum(total, head.usage)
	}
	if total.Calls <= 0 {
		return m.delegateUsage
	}
	return total
}

// usageAgentName is what the pane calls a delegate: the short name its call was given, else the
// header text the block itself shows (the task's first line on an unnamed call), else the constant.
// It is read off the head rather than off the child's report, so a run still working is named the
// same way a finished one is.
func usageAgentName(head entry) string {
	if head.tool.agentName != "" {
		return head.tool.agentName
	}
	if head.tool.Target != "" {
		return head.tool.Target
	}
	return usageAgentFallback
}

// usageRow spells one agent's line: its name, the completions it accounted for, and the tokens they
// carried, with the fill it last reported. Counts go through [format.Tokens] — the coarse form the
// status gauge and a run's own reading are already spelled in — so the three readings on screen are
// read in one language, and a zero leaves its cell empty rather than printing a 0 the column would
// have to be scanned past.
func usageRow(name string, totals usageTotals, used, limit int, cached bool) popupRow {
	calls := ""
	if totals.Calls > 0 {
		calls = strconv.Itoa(totals.Calls)
	}
	row := popupRow{
		name,
		calls,
		format.Tokens(totals.PromptTokens),
	}
	// The cached cell sits INSIDE the prompt count it qualifies, right after it, because that is
	// what it is: the share of those very tokens the server answered from its cache, not a spend
	// beside them. An agent that reported none while another did leaves it empty, on the same rule
	// every other zero on the row follows.
	if cached {
		row = append(row, format.Tokens(totals.CachedPromptTokens))
	}
	return append(row,
		format.Tokens(totals.CompletionTokens),
		format.Tokens(totals.TotalTokens),
		usageFillCell(used, limit),
	)
}

// usageFillCell spells a context reading as the percentage the gauge labels its bar with, clamped
// at 100 for the same reason ([contextUsage.view]): a conversation carried into a smaller window
// overfills it, and a percentage past a hundred is a rendering bug rather than a reading. A missing
// half is no cell at all — a fill without its limit is a number with no scale (subAgentFill).
func usageFillCell(used, limit int) string {
	if used <= 0 || limit <= 0 {
		return ""
	}
	return strconv.Itoa(min(used*100/limit, 100)) + "%"
}

// usageSum adds two agents' totals. Summing across AGENTS is sound where summing across events is
// not: each agent's reading is its own running total, so the parts never overlap.
func usageSum(a, b usageTotals) usageTotals {
	return usageTotals{
		Calls:              a.Calls + b.Calls,
		PromptTokens:       a.PromptTokens + b.PromptTokens,
		CachedPromptTokens: a.CachedPromptTokens + b.CachedPromptTokens,
		CompletionTokens:   a.CompletionTokens + b.CompletionTokens,
		TotalTokens:        a.TotalTokens + b.TotalTokens,
	}
}
