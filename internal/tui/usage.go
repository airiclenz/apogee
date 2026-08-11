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
// dropped event, a resumed session (the main agent's totals ride the session record) and a
// compaction (whose tokens arrive as a flagged maintenance event the gauge skips and the totals
// take).
//
// It is the lightest pane in the frame: no filter, no selection, no keys of its own but esc. The
// verb is whileRunning — the pane reads Model state and calls nothing — so it opens over a working
// agent, which is exactly when the question gets asked.
//
// The POINTER does what the keyboard has no key for (mouse.go): a click outside the box dismisses
// the report, a click inside it is swallowed rather than starting a selection on the transcript
// underneath, and the wheel scrolls a session that fanned out to more delegates than the frame can
// seat rows for. It is a scroll and not a walk, because there is nothing here to select.

// usagePane is the /usage report overlay's state: whether it is up, and how far its row list is
// scrolled. The rows themselves are derived at render time from the folds, so there is nothing here
// to keep in step with them. Its zero value is "closed at the top", so it lives inline in the
// value-copied Model like the picker and the settings pane (ADR 0011).
type usagePane struct {
	open bool
	top  int // the first row the window shows (popupSpec.rowTop) — the wheel's, and nothing else's
}

// usageTitle names the pane, and usageHint spells the one key it owns.
const (
	usageTitle = "session token usage"
	usageHint  = "esc close"
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
// first line of the task where it named none — and five numeric columns beside it need the room
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
var usageHeaderCells = popupRow{"agent", "calls", "prompt", "completion", "total", "ctx"}

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

// closeUsagePane dismisses the report. It is the whole of the pane's key handling (handleKey).
func (m Model) closeUsagePane() (tea.Model, tea.Cmd) {
	return m.dismissUsage(), nil
}

// dismissUsage takes the report off the frame and gives its rows back to the transcript. Both ways of
// closing it spend this one — esc (closeUsagePane) and a click outside the box (handleUsageClick,
// mouse.go) — so the two can never come apart, and neither can leave the scroll behind for the next
// time the report is opened.
func (m Model) dismissUsage() Model {
	m.usagePane = usagePane{}
	m.layout()
	return m
}

// renderUsage paints the pane, or "" when it is closed or the frame cannot seat it.
func (m Model) renderUsage() string {
	if !m.usagePane.open {
		return ""
	}
	spec, seated := m.usageSpec(m.usageRows())
	if !seated {
		return "" // the frame cannot seat this pane beside its siblings (frameRowPlan)
	}
	view, _ := renderPopupPlaced(m.th, spec, m.width)
	return view
}

// usageSpec composes the report's [popupSpec] for THIS frame — the rows, the budget the frame granted
// and the window the wheel scrolled to. ok is false when the frame cannot seat the pane at all.
//
// The rows are composed BEFORE the budget is asked for, because what the pane demands is what it has
// to show — the same order every other overlay composes in (renderPicker, renderSessionBrowser). It
// is a step of its own for the reason the /settings key list's is (settingsKeyListSpec): the painter
// is not the composition's only reader, and the MOUSE has to be told about the very window that was
// drawn (usageWindow, mouse.go) rather than a second derivation of it.
func (m Model) usageSpec(rows []popupRow) (popupSpec, bool) {
	body := ""
	if len(rows) == 0 {
		body = usageEmptyBody
	}
	maxBody, shown, seated := m.popupBudget(paneUsage, len(rows), maxUsageRows, popupChrome, popupFloor{})
	if !seated {
		return popupSpec{}, false
	}
	return popupSpec{
		title:       usageTitle,
		body:        body,
		maxBodyRows: maxBody,
		rows:        rows,
		rowKinds:    usageRowKinds(len(rows)),
		selected:    -1, // a report has no selection: nothing here is chosen (the popup module's convention)
		// The scroll is clamped to the LAST full window rather than to the last row: a report scrolled
		// to its end shows a full pane of rows, and a stale offset — the grant shrank with the window,
		// or a delegate row arrived — is corrected here rather than painting one row over an empty pane.
		rowTop:  clampInt(m.usagePane.top, 0, max(0, len(rows)-shown)),
		hint:    usageHint,
		maxRows: shown,
	}, true
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
// has reported, which is the empty state renderUsage words instead.
//
// A delegate that reported no count is left out rather than shown as a row of blanks: a run whose
// child never got a usage report back is a fact about the server, not a spend, and a row of empty
// cells under the totals would read as one.
func (m Model) usageRows() []popupRow {
	subs, total := m.usageSubAgentRows()
	if m.usage.Calls <= 0 && len(subs) == 0 {
		return nil
	}
	rows := make([]popupRow, 0, len(subs)+3)
	rows = append(rows, usageHeaderCells)
	rows = append(rows, usageRow(usageMainLabel, m.usage, m.ctxUsed, m.opts.ContextWindow))
	rows = append(rows, subs...)
	if len(subs) == 0 {
		return rows
	}
	// The session row is the one number the rows above cannot be read off: an agent's own total is
	// what it spent, and what the SESSION spent is the sum of them. It is drawn only where there is
	// something to sum — with no delegate the main row already is the session — and it carries no
	// fill, because two windows do not add up to a third.
	return append(rows, usageRow(usageSessionLabel, usageSum(m.usage, total), 0, 0))
}

// usageSubAgentRows composes one row per delegate that reported a count, in transcript order — the
// order their blocks stand in, so a reader matches a row to the run above it by position — and
// reports their totals summed for the session row.
func (m Model) usageSubAgentRows() (rows []popupRow, total usageTotals) {
	for i := range m.transcript.entries {
		head := m.transcript.entries[i]
		if head.kind != entryToolCall || head.tool.name != subAgentToolName || head.usage.Calls <= 0 {
			continue
		}
		name, _ := clipCells(m.th, usageIndent+usageAgentName(head), usageNameCells)
		rows = append(rows, usageRow(name, head.usage, head.ctxUsed, head.ctxLimit))
		total = usageSum(total, head.usage)
	}
	return rows, total
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
func usageRow(name string, totals usageTotals, used, limit int) popupRow {
	calls := ""
	if totals.Calls > 0 {
		calls = strconv.Itoa(totals.Calls)
	}
	return popupRow{
		name,
		calls,
		format.Tokens(totals.PromptTokens),
		format.Tokens(totals.CompletionTokens),
		format.Tokens(totals.TotalTokens),
		usageFillCell(used, limit),
	}
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
		Calls:            a.Calls + b.Calls,
		PromptTokens:     a.PromptTokens + b.PromptTokens,
		CompletionTokens: a.CompletionTokens + b.CompletionTokens,
		TotalTokens:      a.TotalTokens + b.TotalTokens,
	}
}
